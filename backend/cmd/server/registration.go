package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mozillazg/go-pinyin"
	"github.com/xuri/excelize/v2"
	"golang.org/x/image/draw"
)

type rosterRow struct {
	GroupCode string `json:"group_code"`
	GroupName string `json:"group_name"`
	Name      string `json:"name"`
	Identity  string `json:"identity_key"`
	IsLeader  bool   `json:"is_leader"`
	IsMinor   bool   `json:"is_minor"`
}

var rosterSheetCodes = map[string]string{
	"真理_DT+ZWTJ": "truth-dt-zwtj",
	"圣经_DT":      "bible-dt",
	"生命_ZWTJ":    "life-zwtj",
	"圣经_ZWWX":    "bible-zwwx",
	"圣经_KD":      "bible-kd",
	"圣经_AGP":     "bible-agp",
	"圣经_GD":      "bible-gd",
	"CZ_道路":      "cz-way",
}

func canonicalRosterName(value string) (string, bool) {
	v := strings.TrimSpace(value)
	minor := strings.Contains(v, "辅修")
	v = strings.ReplaceAll(v, "（辅修）", "")
	v = strings.ReplaceAll(v, "(辅修)", "")
	v = strings.TrimSpace(v)
	return v, minor
}

func identityKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), ""))
}

func usernameFromName(name string) string {
	overrides := map[string]string{
		"张迦勒": "zhangjiale", "陈思佳": "chensijia", "廖美倩": "liaomeiqian",
		"苏相宜": "suxiangyi", "李群": "liqun", "戴许诺": "daixunuo",
		"许水英": "xushuiying", "贺丽华": "helihua", "朱灵": "zhuling",
		"李思思": "lisisi", "何金群": "hejinqun", "胡方舟": "hufangzhou",
		"戴维多尔": "daiweiduoer", "仇健棒": "qiujianbang", "彭朋": "pengpeng",
	}
	if value := overrides[name]; value != "" {
		return value
	}
	args := pinyin.NewArgs()
	args.Style = pinyin.Normal
	args.Heteronym = false
	var b strings.Builder
	for _, part := range pinyin.Pinyin(name, args) {
		if len(part) > 0 {
			b.WriteString(part[0])
		}
	}
	clean := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(b.String()), "")
	if clean == "" {
		clean = "member"
	}
	return clean
}

func (a *app) uniqueUsername(tx *sql.Tx, name string) (string, error) {
	base := usernameFromName(name)
	for i := 0; i < 10000; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s%d", base, i+1)
		}
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE LOWER(username)=?", candidate).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("username_exhausted")
}

func parseRoster(path string) ([]rosterRow, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []rosterRow
	for _, sheet := range f.GetSheetList() {
		code, ok := rosterSheetCodes[strings.TrimSpace(sheet)]
		if !ok {
			continue
		}
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, err
		}
		for index, row := range rows {
			if index == 0 || len(row) < 2 || strings.TrimSpace(row[1]) == "" {
				continue
			}
			name, minor := canonicalRosterName(row[1])
			styleID, _ := f.GetCellStyle(sheet, fmt.Sprintf("B%d", index+1))
			style, _ := f.GetStyle(styleID)
			color := ""
			if style != nil && style.Font != nil {
				color = strings.ToUpper(strings.TrimPrefix(style.Font.Color, "#"))
			}
			leader := color == "FF0000" || color == "FFFF0000"
			out = append(out, rosterRow{GroupCode: code, GroupName: sheet, Name: name, Identity: identityKey(name), IsLeader: leader, IsMinor: minor})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no_supported_roster_sheets")
	}
	return out, nil
}

func (a *app) bootstrapRoster() error {
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM roster_entries").Scan(&count); err != nil || count > 0 {
		return err
	}
	if _, err := os.Stat(a.rosterPath); err != nil {
		return err
	}
	rows, err := parseRoster(a.rosterPath)
	if err != nil {
		return err
	}
	data, _ := os.ReadFile(a.rosterPath)
	sum := sha256.Sum256(data)
	return a.syncRoster(rows, filepath.Base(a.rosterPath), hex.EncodeToString(sum[:]), 0)
}

func (a *app) syncRoster(rows []rosterRow, filename, checksum string, actor uint64) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowSQL()
	var actorValue any
	if actor > 0 {
		actorValue = actor
	}
	res, err := tx.Exec("INSERT INTO roster_imports(filename,checksum_sha256,imported_by,row_count,created_at) VALUES(?,?,?,?,?)", filename, checksum, actorValue, len(rows), now)
	if err != nil {
		return err
	}
	importID, _ := res.LastInsertId()
	seen := map[string]bool{}
	for _, row := range rows {
		_, err = tx.Exec(`INSERT INTO study_groups(code,name,description,status,created_by,created_at,updated_at)
			VALUES(?,?,?,1,?,?,?) ON DUPLICATE KEY UPDATE name=VALUES(name),status=1,updated_at=VALUES(updated_at)`, row.GroupCode, row.GroupName, "2026年度门训生命季", actorValue, now, now)
		if err != nil {
			return err
		}
		var groupID uint64
		if err = tx.QueryRow("SELECT id FROM study_groups WHERE code=?", row.GroupCode).Scan(&groupID); err != nil {
			return err
		}
		key := fmt.Sprintf("%d:%s", groupID, row.Identity)
		seen[key] = true
		_, err = tx.Exec(`INSERT INTO roster_entries(import_id,group_id,canonical_name,identity_key,is_leader,is_minor,status,created_at,updated_at)
			VALUES(?,?,?,?,?,?,1,?,?) ON DUPLICATE KEY UPDATE import_id=VALUES(import_id),canonical_name=VALUES(canonical_name),is_leader=VALUES(is_leader),is_minor=VALUES(is_minor),status=1,updated_at=VALUES(updated_at)`, importID, groupID, row.Name, row.Identity, row.IsLeader, row.IsMinor, now, now)
		if err != nil {
			return err
		}
	}
	current, err := tx.Query("SELECT id,group_id,identity_key,claimed_by_user_id,is_leader FROM roster_entries WHERE status=1")
	if err != nil {
		return err
	}
	type stale struct {
		id, gid uint64
		ident   string
		claimed sql.NullInt64
		leader  bool
	}
	var staleRows []stale
	for current.Next() {
		var s stale
		_ = current.Scan(&s.id, &s.gid, &s.ident, &s.claimed, &s.leader)
		if !seen[fmt.Sprintf("%d:%s", s.gid, s.ident)] {
			staleRows = append(staleRows, s)
		}
	}
	current.Close()
	for _, s := range staleRows {
		if _, err = tx.Exec("UPDATE roster_entries SET status=0,updated_at=? WHERE id=?", now, s.id); err != nil {
			return err
		}
		if s.claimed.Valid {
			_, _ = tx.Exec("UPDATE group_members SET status=0,updated_at=? WHERE group_id=? AND user_id=?", now, s.gid, s.claimed.Int64)
			if s.leader {
				_, _ = tx.Exec("DELETE FROM user_group_roles WHERE group_id=? AND user_id=? AND role=?", s.gid, s.claimed.Int64, roleGroupLeader)
			}
		}
	}
	// Apply leader changes immediately to already claimed accounts.
	_, _ = tx.Exec(`INSERT IGNORE INTO user_group_roles(group_id,user_id,role,created_at)
		SELECT group_id,claimed_by_user_id,?,? FROM roster_entries WHERE status=1 AND is_leader=1 AND claimed_by_user_id IS NOT NULL`, roleGroupLeader, now)
	_, _ = tx.Exec(`DELETE r FROM user_group_roles r JOIN roster_entries e ON e.group_id=r.group_id AND e.claimed_by_user_id=r.user_id
		WHERE r.role=? AND e.status=1 AND e.is_leader=0`, roleGroupLeader)
	return tx.Commit()
}

func (a *app) handleRegistrationGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT DISTINCT g.id,g.code,g.name FROM roster_entries e JOIN study_groups g ON g.id=e.group_id WHERE e.status=1 AND g.status=1 ORDER BY g.id`)
	if err != nil {
		writeError(w, 500, "groups_failed")
		return
	}
	defer rows.Close()
	groups := []group{}
	for rows.Next() {
		var g group
		_ = rows.Scan(&g.ID, &g.Code, &g.Name)
		groups = append(groups, g)
	}
	writeJSON(w, 200, map[string]any{"groups": groups})
}

func (a *app) rosterMatch(name string, groupID uint64) (string, bool, bool, error) {
	canonical, _ := canonicalRosterName(name)
	ident := identityKey(canonical)
	var stored string
	var claimed sql.NullInt64
	err := a.db.QueryRow("SELECT canonical_name,claimed_by_user_id FROM roster_entries WHERE group_id=? AND identity_key=? AND status=1", groupID, ident).Scan(&stored, &claimed)
	return stored, claimed.Valid, ident != "", err
}

func (a *app) handleRegistrationPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		GroupID uint64 `json:"group_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name, claimed, _, err := a.rosterMatch(req.Name, req.GroupID)
	if err != nil {
		writeError(w, 404, "roster_not_found")
		return
	}
	writeJSON(w, 200, map[string]any{"matched": true, "canonical_name": name, "suggested_username": usernameFromName(name), "claimed": claimed})
}

func validEmail(v string) bool {
	i := strings.LastIndex(v, "@")
	return i > 0 && i < len(v)-3 && strings.Contains(v[i+1:], ".")
}

func (a *app) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		GroupID  uint64 `json:"group_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validEmail(email) {
		writeError(w, 400, "invalid_email")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, 400, "weak_password")
		return
	}
	canonical, claimed, _, err := a.rosterMatch(req.Name, req.GroupID)
	if err != nil {
		writeError(w, 400, "roster_not_found")
		return
	}
	if claimed {
		writeError(w, 409, "roster_already_claimed")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, 500, "register_failed")
		return
	}
	defer tx.Rollback()
	ident := identityKey(canonical)
	var n int
	_ = tx.QueryRow("SELECT COUNT(*) FROM users WHERE LOWER(email)=?", email).Scan(&n)
	if n > 0 {
		writeError(w, 409, "email_exists")
		return
	}
	username, err := a.uniqueUsername(tx, canonical)
	if err != nil {
		writeError(w, 500, "register_failed")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeError(w, 500, "register_failed")
		return
	}
	now := nowSQL()
	res, err := tx.Exec(`INSERT INTO users(username,display_name,name_pinyin,password_hash,email,email_normalized,default_group_id,status,created_at,updated_at,profile_updated_at) VALUES(?,?,?,?,?,?,?,1,?,?,?)`, username, canonical, username, hash, email, email, req.GroupID, now, now, now)
	if err != nil {
		writeError(w, 409, "account_exists")
		return
	}
	uid64, _ := res.LastInsertId()
	uid := uint64(uid64)
	rows, err := tx.Query("SELECT id,group_id,canonical_name,is_leader FROM roster_entries WHERE identity_key=? AND status=1 FOR UPDATE", ident)
	if err != nil {
		writeError(w, 500, "register_failed")
		return
	}
	type entry struct {
		id, gid uint64
		name    string
		leader  bool
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if rows.Scan(&e.id, &e.gid, &e.name, &e.leader) == nil {
			entries = append(entries, e)
		}
	}
	rows.Close()
	for _, e := range entries {
		res, err = tx.Exec("UPDATE roster_entries SET claimed_by_user_id=?,updated_at=? WHERE id=? AND claimed_by_user_id IS NULL", uid, now, e.id)
		if err != nil {
			writeError(w, 500, "register_failed")
			return
		}
		affected, _ := res.RowsAffected()
		if affected != 1 {
			writeError(w, 409, "roster_already_claimed")
			return
		}
		_, err = tx.Exec(`INSERT INTO group_members(group_id,user_id,member_name,status,joined_at,created_at,updated_at) VALUES(?,?,?,1,?,?,?) ON DUPLICATE KEY UPDATE status=1,member_name=VALUES(member_name),updated_at=VALUES(updated_at)`, e.gid, uid, e.name, now, now, now)
		if err != nil {
			writeError(w, 500, "register_failed")
			return
		}
		if e.leader {
			_, err = tx.Exec("INSERT IGNORE INTO user_group_roles(group_id,user_id,role,created_at) VALUES(?,?,?,?)", e.gid, uid, roleGroupLeader, now)
			if err != nil {
				writeError(w, 500, "register_failed")
				return
			}
		}
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "register_failed")
		return
	}
	u, _ := a.loadCurrentUser(uid, req.GroupID)
	token, _ := a.signToken(tokenClaims{UserID: uid, CurrentGroupID: req.GroupID})
	writeJSON(w, 201, map[string]any{"token": token, "user": u, "username": username})
}

func (a *app) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !regexp.MustCompile(`^[a-z][a-z0-9._-]{2,63}$`).MatchString(username) {
		writeError(w, 400, "invalid_username")
		return
	}
	if !validEmail(email) {
		writeError(w, 400, "invalid_email")
		return
	}
	_, err := a.db.Exec("UPDATE users SET username=?,name_pinyin=?,email=?,email_normalized=?,profile_updated_at=?,updated_at=? WHERE id=?", username, username, email, email, nowSQL(), nowSQL(), u.ID)
	if err != nil {
		writeError(w, 409, "profile_conflict")
		return
	}
	updated, _ := a.loadCurrentUser(u.ID, u.CurrentGroupID)
	writeJSON(w, 200, map[string]any{"user": updated})
}

func (a *app) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		writeError(w, 400, "avatar_too_large")
		return
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		writeError(w, 400, "avatar_required")
		return
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		writeError(w, 400, "invalid_avatar")
		return
	}
	b := img.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	src := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, b.Min.X+(b.Dx()+side)/2, b.Min.Y+(b.Dy()+side)/2)
	dst := image.NewRGBA(image.Rect(0, 0, 512, 512))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, src, draw.Over, nil)
	dir := filepath.Join(a.assetsRoot, "avatars")
	if err = os.MkdirAll(dir, 0755); err != nil {
		writeError(w, 500, "avatar_failed")
		return
	}
	name := fmt.Sprintf("%d-%d.jpg", u.ID, time.Now().UnixNano())
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		writeError(w, 500, "avatar_failed")
		return
	}
	err = jpeg.Encode(out, dst, &jpeg.Options{Quality: 86})
	out.Close()
	if err != nil {
		writeError(w, 500, "avatar_failed")
		return
	}
	_, err = a.db.Exec("UPDATE users SET avatar_path=?,profile_updated_at=?,updated_at=? WHERE id=?", name, nowSQL(), nowSQL(), u.ID)
	if err != nil {
		writeError(w, 500, "avatar_failed")
		return
	}
	writeJSON(w, 200, map[string]any{"avatar_url": "/api/avatars/" + name})
}

func (a *app) handleAvatar(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if name == "." || strings.ContainsAny(name, "/\\") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(a.assetsRoot, "avatars", name))
}

func saveRosterUpload(w http.ResponseWriter, r *http.Request) (string, string, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		return "", "", "", err
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		return "", "", "", err
	}
	defer f.Close()
	tmp, err := os.CreateTemp("", "agp-roster-*.xlsx")
	if err != nil {
		return "", "", "", err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, hash), f)
	tmp.Close()
	if err != nil {
		os.Remove(tmp.Name())
		return "", "", "", err
	}
	return tmp.Name(), h.Filename, hex.EncodeToString(hash.Sum(nil)), nil
}

func (a *app) handleSuperRoster(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT e.id,e.canonical_name,e.is_leader,e.is_minor,e.status,e.claimed_by_user_id,g.id,g.name,u.username,u.email FROM roster_entries e JOIN study_groups g ON g.id=e.group_id LEFT JOIN users u ON u.id=e.claimed_by_user_id ORDER BY g.id,e.id`)
	if err != nil {
		writeError(w, 500, "roster_failed")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, gid uint64
		var name, gname string
		var leader, minor bool
		var status int
		var claimed sql.NullInt64
		var username, email sql.NullString
		_ = rows.Scan(&id, &name, &leader, &minor, &status, &claimed, &gid, &gname, &username, &email)
		items = append(items, map[string]any{"id": id, "name": name, "group_id": gid, "group_name": gname, "is_leader": leader, "is_minor": minor, "status": status, "claimed_by_user_id": nullableUint64(claimed), "username": username.String, "email": email.String})
	}
	writeJSON(w, 200, map[string]any{"entries": items})
}

func (a *app) handleSuperRosterPreview(w http.ResponseWriter, r *http.Request) {
	path, name, checksum, err := saveRosterUpload(w, r)
	if err != nil {
		writeError(w, 400, "invalid_roster_file")
		return
	}
	defer os.Remove(path)
	rows, err := parseRoster(path)
	if err != nil {
		writeError(w, 400, "invalid_roster_file")
		return
	}
	leaders := 0
	for _, row := range rows {
		if row.IsLeader {
			leaders++
		}
	}
	writeJSON(w, 200, map[string]any{"filename": name, "checksum": checksum, "row_count": len(rows), "leader_count": leaders, "entries": rows})
}

func (a *app) handleSuperRosterImport(w http.ResponseWriter, r *http.Request) {
	path, name, checksum, err := saveRosterUpload(w, r)
	if err != nil {
		writeError(w, 400, "invalid_roster_file")
		return
	}
	defer os.Remove(path)
	rows, err := parseRoster(path)
	if err != nil {
		writeError(w, 400, "invalid_roster_file")
		return
	}
	if err = a.syncRoster(rows, name, checksum, mustUser(r).ID); err != nil {
		writeError(w, 500, "roster_import_failed")
		return
	}
	a.audit(0, mustUser(r).ID, "import_roster", "roster_entries", 0, nil, map[string]any{"filename": name, "rows": len(rows)}, r)
	writeJSON(w, 200, map[string]any{"imported": len(rows)})
}

func (a *app) handleSuperRosterEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       uint64 `json:"id"`
		GroupID  uint64 `json:"group_id"`
		Name     string `json:"name"`
		IsLeader bool   `json:"is_leader"`
		IsMinor  bool   `json:"is_minor"`
		Status   int    `json:"status"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name, _ := canonicalRosterName(req.Name)
	if name == "" || req.GroupID == 0 {
		writeError(w, 400, "invalid_roster_entry")
		return
	}
	now := nowSQL()
	if req.Status == 0 {
		req.Status = 1
	}
	if req.ID == 0 {
		_, err := a.db.Exec("INSERT INTO roster_entries(group_id,canonical_name,identity_key,is_leader,is_minor,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", req.GroupID, name, identityKey(name), req.IsLeader, req.IsMinor, req.Status, now, now)
		if err != nil {
			writeError(w, 409, "roster_conflict")
			return
		}
	} else {
		_, err := a.db.Exec("UPDATE roster_entries SET group_id=?,canonical_name=?,identity_key=?,is_leader=?,is_minor=?,status=?,updated_at=? WHERE id=?", req.GroupID, name, identityKey(name), req.IsLeader, req.IsMinor, req.Status, now, req.ID)
		if err != nil {
			writeError(w, 409, "roster_conflict")
			return
		}
	}
	var claimed sql.NullInt64
	_ = a.db.QueryRow("SELECT claimed_by_user_id FROM roster_entries WHERE group_id=? AND identity_key=?", req.GroupID, identityKey(name)).Scan(&claimed)
	if claimed.Valid {
		if req.IsLeader {
			_, _ = a.db.Exec("INSERT IGNORE INTO user_group_roles(group_id,user_id,role,created_at) VALUES(?,?,?,?)", req.GroupID, claimed.Int64, roleGroupLeader, now)
		} else {
			_, _ = a.db.Exec("DELETE FROM user_group_roles WHERE group_id=? AND user_id=? AND role=?", req.GroupID, claimed.Int64, roleGroupLeader)
		}
	}
	a.audit(req.GroupID, mustUser(r).ID, "save_roster_entry", "roster_entries", req.ID, nil, map[string]any{"name": name, "is_leader": req.IsLeader, "is_minor": req.IsMinor, "status": req.Status}, r)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func onlyLettersAndNumbers(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}
