package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	mysqlDriver "github.com/go-sql-driver/mysql"
	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/xuri/excelize/v2"
)

const (
	roleMember               = "member"
	roleGroupAdmin           = "group_admin"
	roleGroupLeader          = "group_leader"
	appTZName                = "Asia/Shanghai"
	tokenTTL                 = 30 * 24 * time.Hour
	maxUploadBytes           = int64(512 << 20)
	maxBackupBytes           = int64(64 << 20)
	maxStudyWeeksImportBytes = int64(32 << 20)
	loginFailureTTL          = 30 * time.Minute
	loginFailureCap          = 5000
	maxAvatarSide            = 8192
	maxAvatarPixels          = int64(24_000_000)
)

type app struct {
	db            *sql.DB
	secret        []byte
	assetsRoot    string
	contentRoot   string
	migrationsDir string
	location      *time.Location
	loginLimiter  *loginLimiter
	rosterPath    string
}

type config struct {
	Addr                 string
	DSN                  string
	JWTSecret            string
	AssetsRoot           string
	ContentRoot          string
	MigrationsDir        string
	BootstrapUsername    string
	BootstrapPassword    string
	BootstrapDisplayName string
	RosterPath           string
}

type ctxKey string

const currentUserKey ctxKey = "currentUser"

type currentUser struct {
	ID                 uint64   `json:"id"`
	Username           string   `json:"username"`
	DisplayName        string   `json:"display_name"`
	IsSuperAdmin       bool     `json:"is_super_admin"`
	DefaultGroupID     uint64   `json:"default_group_id"`
	MustChangePassword bool     `json:"must_change_password"`
	AuthVersion        uint64   `json:"-"`
	CurrentGroupID     uint64   `json:"current_group_id"`
	Groups             []group  `json:"study_groups"`
	Roles              []string `json:"roles"`
	Email              string   `json:"email"`
	AvatarURL          string   `json:"avatar_url"`
}

type group struct {
	ID   uint64 `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type weekTaskBinding struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Type    string `json:"type"`
	AssetID uint64 `json:"asset_id"`
}

type studyWeekInput struct {
	StartDate      string            `json:"start_date"`
	EndDate        string            `json:"end_date"`
	Title          string            `json:"title"`
	VerseRef       string            `json:"verse_ref"`
	ReciteText     string            `json:"recite_text"`
	BookEnabled    bool              `json:"book_enabled"`
	VideoEnabled   bool              `json:"video_enabled"`
	VerseEnabled   bool              `json:"verse_enabled"`
	OutlineEnabled bool              `json:"outline_enabled"`
	Readings       []weekTaskBinding `json:"readings"`
	Videos         []weekTaskBinding `json:"videos"`
	Outline        weekTaskBinding   `json:"outline"`
}

type tokenClaims struct {
	UserID         uint64 `json:"uid"`
	CurrentGroupID uint64 `json:"gid,omitempty"`
	AuthVersion    uint64 `json:"ver"`
	ExpiresAt      int64  `json:"exp"`
}

func main() {
	cfg := loadConfig()
	if len(cfg.JWTSecret) < 32 || strings.Contains(strings.ToUpper(cfg.JWTSecret), "CHANGE_ME") || cfg.JWTSecret == "dev-secret-change-me" || cfg.JWTSecret == "please-change-this-to-a-long-random-string" {
		log.Fatal("AGP_JWT_SECRET must be a random value of at least 32 characters")
	}
	if len(cfg.BootstrapPassword) < 12 || strings.Contains(strings.ToUpper(cfg.BootstrapPassword), "CHANGE_ME") || cfg.BootstrapPassword == "ChangeMe123" {
		log.Fatal("BOOTSTRAP_SUPERADMIN_PASSWORD must be set to at least 12 characters")
	}
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	loc, err := time.LoadLocation(appTZName)
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	a := &app{
		db:            db,
		secret:        []byte(cfg.JWTSecret),
		assetsRoot:    cfg.AssetsRoot,
		contentRoot:   cfg.ContentRoot,
		migrationsDir: cfg.MigrationsDir,
		location:      loc,
		loginLimiter:  newLoginLimiter(),
		rosterPath:    cfg.RosterPath,
	}
	if err := a.runMigrations(); err != nil {
		log.Fatal(err)
	}
	if err := a.ensureFuturePartitions(time.Now().In(loc), 2); err != nil {
		log.Printf("partition maintenance failed: %v", err)
	}
	if err := a.bootstrapSuperAdmin(cfg); err != nil {
		log.Fatal(err)
	}
	if err := a.bootstrapRoster(); err != nil {
		log.Printf("roster bootstrap skipped: %v", err)
	}

	mux := http.NewServeMux()
	a.routes(mux)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withRecovery(withCommonHeaders(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Minute,
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("AGP backend listening on %s", cfg.Addr)
	select {
	case sig := <-shutdownSignals:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

func loadConfig() config {
	return config{
		Addr:                 env("AGP_ADDR", ":8080"),
		DSN:                  env("AGP_DSN", "agp:agp@tcp(127.0.0.1:3306)/agp?parseTime=true&multiStatements=false&charset=utf8mb4,utf8"),
		JWTSecret:            env("AGP_JWT_SECRET", "dev-secret-change-me"),
		AssetsRoot:           env("AGP_ASSETS_ROOT", "/data/agp/assets"),
		ContentRoot:          env("AGP_CONTENT_ROOT", "/data/agp/content"),
		MigrationsDir:        env("AGP_MIGRATIONS_DIR", "./migrations"),
		BootstrapUsername:    env("BOOTSTRAP_SUPERADMIN_USERNAME", "admin"),
		BootstrapPassword:    env("BOOTSTRAP_SUPERADMIN_PASSWORD", "ChangeMe123"),
		RosterPath:           env("AGP_ROSTER_PATH", "../2026年度门训生命季_分组及组长.xlsx"),
		BootstrapDisplayName: env("BOOTSTRAP_SUPERADMIN_DISPLAY_NAME", "超级管理员"),
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (a *app) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("GET /api/auth/registration-groups", a.handleRegistrationGroups)
	mux.HandleFunc("POST /api/auth/registration-preview", a.handleRegistrationPreview)
	mux.HandleFunc("POST /api/auth/register", a.handleRegister)
	mux.HandleFunc("GET /api/auth/me", a.auth(a.handleMe))
	mux.HandleFunc("PUT /api/auth/profile", a.auth(a.handleUpdateProfile))
	mux.HandleFunc("POST /api/auth/avatar", a.auth(a.handleUploadAvatar))
	mux.HandleFunc("GET /api/avatars/{name}", a.handleAvatar)
	mux.HandleFunc("POST /api/auth/switch-group", a.auth(a.handleSwitchGroup))
	mux.HandleFunc("POST /api/auth/default-group", a.auth(a.handleSetDefaultGroup))
	mux.HandleFunc("POST /api/auth/change-password", a.auth(a.handleChangePassword))

	mux.HandleFunc("GET /api/app/bootstrap", a.auth(a.handleBootstrap))
	mux.HandleFunc("GET /api/dashboard/summary", a.auth(a.handleDashboardSummary))
	mux.HandleFunc("GET /api/dashboard/monthly-ranking", a.auth(a.handleDashboardMonthlyRanking))
	mux.HandleFunc("GET /api/members", a.auth(a.handleMembers))
	mux.HandleFunc("GET /api/members/{id}/calendar", a.auth(a.handleMemberCalendar))

	mux.HandleFunc("GET /api/study-weeks", a.auth(a.handleStudyWeeks))
	mux.HandleFunc("GET /api/study-weeks/current", a.auth(a.handleCurrentStudyWeek))
	mux.HandleFunc("POST /api/admin/study-weeks", a.auth(a.requireRole(roleGroupLeader, a.handleAdminCreateStudyWeek)))
	mux.HandleFunc("PUT /api/admin/study-weeks/{id}", a.auth(a.requireRole(roleGroupLeader, a.handleAdminUpdateStudyWeek)))
	mux.HandleFunc("DELETE /api/admin/study-weeks/{id}", a.auth(a.requireRole(roleGroupLeader, a.handleAdminDeleteStudyWeek)))

	mux.HandleFunc("POST /api/checkins", a.auth(a.handleCreateCheckin))
	mux.HandleFunc("DELETE /api/checkins/{id}", a.auth(a.handleDeleteOwnCheckin))
	mux.HandleFunc("GET /api/checkins", a.auth(a.handleListCheckins))
	mux.HandleFunc("DELETE /api/admin/checkins/{id}", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminDeleteCheckin)))

	mux.HandleFunc("GET /api/assets", a.auth(a.handleListAssets))
	mux.HandleFunc("GET /api/resource-library", a.auth(a.handleResourceLibrary))
	mux.HandleFunc("GET /api/assets/{id}/download", a.auth(a.handleDownloadAsset))
	mux.HandleFunc("GET /api/assets/{id}/range", a.auth(a.handleDownloadAssetRange))
	mux.HandleFunc("POST /api/admin/assets/upload", a.auth(a.requireRole(roleGroupLeader, a.handleAdminUploadAsset)))
	mux.HandleFunc("GET /api/admin/resource-library", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminResourceLibrary)))
	mux.HandleFunc("PUT /api/admin/resource-library/visibility", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminResourceLibraryVisibility)))
	mux.HandleFunc("POST /api/admin/resource-folders", a.auth(a.requireSuper(a.handleAdminCreateResourceFolder)))
	mux.HandleFunc("PUT /api/admin/resource-folders", a.auth(a.requireSuper(a.handleAdminRenameResourceFolder)))
	mux.HandleFunc("GET /api/admin/learning-config", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminLearningConfig)))
	mux.HandleFunc("PUT /api/admin/learning-config", a.auth(a.requireRole(roleGroupLeader, a.handleAdminSaveLearningConfig)))
	mux.HandleFunc("GET /api/content/pdf-range", a.auth(a.handleStaticPDFRange))
	mux.HandleFunc("GET /api/admin/exports/checkins-detail", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminExportCheckinsCSV)))
	mux.HandleFunc("GET /api/admin/exports/daily-summary", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminExportDailySummaryCSV)))
	mux.HandleFunc("GET /api/admin/exports/study-weeks", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminExportStudyWeeksExcel)))
	mux.HandleFunc("POST /api/admin/imports/study-weeks", a.auth(a.requireRole(roleGroupLeader, a.handleAdminImportStudyWeeksExcel)))
	mux.HandleFunc("GET /api/admin/exports/feedbacks", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminExportFeedbacksCSV)))
	mux.HandleFunc("GET /api/admin/exports/local-backup", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminExportLocalBackupJSON)))
	mux.HandleFunc("POST /api/admin/imports/local-backup", a.auth(a.requireRole(roleGroupLeader, a.handleAdminImportLocalBackupJSON)))
	mux.HandleFunc("POST /api/admin/members", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminCreateMember)))
	mux.HandleFunc("DELETE /api/admin/members/{id}", a.auth(a.requireRole(roleGroupAdmin, a.handleAdminRemoveMember)))
	mux.HandleFunc("PUT /api/admin/group/default-password", a.auth(a.requireRole(roleGroupLeader, a.handleAdminSetGroupDefaultPassword)))
	mux.HandleFunc("POST /api/admin/members/{id}/admins", a.auth(a.requireRole(roleGroupAdmin, a.handleGrantGroupAdmin)))
	mux.HandleFunc("DELETE /api/admin/members/{id}/admins", a.auth(a.requireRole(roleGroupAdmin, a.handleRevokeGroupAdmin)))
	mux.HandleFunc("GET /api/admin/audit-logs", a.auth(a.requireRole(roleGroupAdmin, a.handleAuditLogs)))

	mux.HandleFunc("GET /api/super-admin/groups", a.auth(a.requireSuper(a.handleSuperListGroups)))
	mux.HandleFunc("POST /api/super-admin/groups", a.auth(a.requireSuper(a.handleSuperCreateGroup)))
	mux.HandleFunc("POST /api/super-admin/groups/{id}/default-password", a.auth(a.requireSuper(a.handleSuperSetGroupDefaultPassword)))
	mux.HandleFunc("GET /api/super-admin/users", a.auth(a.requireSuper(a.handleSuperListUsers)))
	mux.HandleFunc("GET /api/super-admin/roster", a.auth(a.requireSuper(a.handleSuperRoster)))
	mux.HandleFunc("POST /api/super-admin/roster/preview", a.auth(a.requireSuper(a.handleSuperRosterPreview)))
	mux.HandleFunc("POST /api/super-admin/roster/import", a.auth(a.requireSuper(a.handleSuperRosterImport)))
	mux.HandleFunc("POST /api/super-admin/roster/entries", a.auth(a.requireSuper(a.handleSuperRosterEntry)))
	mux.HandleFunc("POST /api/super-admin/users", a.auth(a.requireSuper(a.handleSuperCreateUser)))
	mux.HandleFunc("POST /api/super-admin/users/reset-all-passwords", a.auth(a.requireSuper(a.handleSuperResetAllPasswords)))
	mux.HandleFunc("POST /api/super-admin/groups/{id}/members", a.auth(a.requireSuper(a.handleSuperAddGroupMember)))
	mux.HandleFunc("POST /api/super-admin/groups/{id}/leaders", a.auth(a.requireSuper(a.handleSuperSetLeader)))
	mux.HandleFunc("DELETE /api/super-admin/groups/{id}/leaders/{user_id}", a.auth(a.requireSuper(a.handleSuperUnsetLeader)))
}

func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, recovered)
				writeError(w, http.StatusInternalServerError, "internal_error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *app) runMigrations() error {
	if _, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename VARCHAR(255) PRIMARY KEY,
		applied_at DATETIME(3) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := os.ReadDir(a.migrationsDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied int
		if err := a.db.QueryRow("SELECT COUNT(1) FROM schema_migrations WHERE filename=?", entry.Name()).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		if applied > 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(a.migrationsDir, entry.Name()))
		if err != nil {
			return err
		}
		for _, stmt := range splitSQL(string(data)) {
			if _, err := a.db.Exec(stmt); err != nil {
				if isIgnorableMigrationError(err) {
					log.Printf("migration %s skipped existing schema object: %v", entry.Name(), err)
					continue
				}
				return fmt.Errorf("%s: %w", entry.Name(), err)
			}
		}
		if _, err := a.db.Exec("INSERT INTO schema_migrations(filename,applied_at) VALUES(?,?)", entry.Name(), nowSQL()); err != nil {
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func isIgnorableMigrationError(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	// DDL in MySQL commits implicitly. If a previous startup stopped between
	// statements, retrying must be able to finish the remaining statements.
	return mysqlErr.Number == 1060 || mysqlErr.Number == 1061
}

func splitSQL(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ";") {
		stmt := strings.TrimSpace(part)
		if stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

func (a *app) ensureFuturePartitions(now time.Time, quartersAhead int) error {
	for i := 0; i <= quartersAhead; i++ {
		start := quarterStart(now).AddDate(0, i*3, 0)
		name := fmt.Sprintf("p%dq%d", start.Year(), int(start.Month()-1)/3+1)
		lessThan := start.AddDate(0, 3, 0).Format("2006-01-02")
		var exists int
		err := a.db.QueryRow(`
			SELECT COUNT(*) FROM information_schema.PARTITIONS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'checkin_records' AND PARTITION_NAME = ?`, name).Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE checkin_records REORGANIZE PARTITION pmax INTO (PARTITION %s VALUES LESS THAN ('%s'), PARTITION pmax VALUES LESS THAN (MAXVALUE))", name, lessThan)
		if _, err := a.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func quarterStart(t time.Time) time.Time {
	month := time.Month((int(t.Month())-1)/3*3 + 1)
	return time.Date(t.Year(), month, 1, 0, 0, 0, 0, t.Location())
}

func (a *app) bootstrapSuperAdmin(cfg config) error {
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM users WHERE is_super_admin = 1").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := hashPassword(cfg.BootstrapPassword)
	if err != nil {
		return err
	}
	now := nowSQL()
	_, err = a.db.Exec(`INSERT INTO users (username, display_name, name_pinyin, password_hash, is_super_admin, must_change_password, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, 1, ?, ?)`, cfg.BootstrapUsername, cfg.BootstrapDisplayName, cfg.BootstrapUsername, hash, now, now)
	return err
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "database": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "database": "ready"})
}

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username   string `json:"username"`
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(strings.ToLower(firstNonEmpty(req.Identifier, req.Username)))
	remote := clientIP(r)
	if a.loginLimiter.blocked(remote, username) {
		writeError(w, http.StatusTooManyRequests, "too_many_attempts")
		return
	}
	var user struct {
		ID                 uint64
		Username           string
		DisplayName        string
		PasswordHash       string
		IsSuperAdmin       bool
		DefaultGroupID     sql.NullInt64
		MustChangePassword bool
		AuthVersion        uint64
		Status             int
	}
	err := a.db.QueryRow(`SELECT id, username, display_name, password_hash, is_super_admin, default_group_id, must_change_password, auth_version, status FROM users WHERE LOWER(username) = ? OR LOWER(email) = ? LIMIT 1`, username, username).
		Scan(&user.ID, &user.Username, &user.DisplayName, &user.PasswordHash, &user.IsSuperAdmin, &user.DefaultGroupID, &user.MustChangePassword, &user.AuthVersion, &user.Status)
	if err != nil || user.Status != 1 || !verifyPassword(req.Password, user.PasswordHash) {
		a.loginLimiter.fail(remote, username)
		writeError(w, http.StatusUnauthorized, "invalid_username_or_password")
		return
	}
	a.loginLimiter.success(remote, username)
	groups, _ := a.visibleGroups(user.ID, user.IsSuperAdmin)
	var currentGroupID uint64
	if user.DefaultGroupID.Valid && containsGroup(groups, uint64(user.DefaultGroupID.Int64)) {
		currentGroupID = uint64(user.DefaultGroupID.Int64)
	} else if len(groups) == 1 {
		currentGroupID = groups[0].ID
	}
	token, err := a.signToken(newTokenClaims(user.ID, currentGroupID, user.AuthVersion))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed")
		return
	}
	_, _ = a.db.Exec("UPDATE users SET last_login_at = ? WHERE id = ?", nowSQL(), user.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user": map[string]any{
			"id": user.ID, "username": user.Username, "display_name": user.DisplayName,
			"is_super_admin": user.IsSuperAdmin, "default_group_id": nullableUint64(user.DefaultGroupID),
			"must_change_password": user.MustChangePassword,
			"current_group_id":     currentGroupID, "study_groups": groups,
		},
	})
}

func (a *app) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"user": mustUser(r)})
}

func (a *app) handleSwitchGroup(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	var req struct {
		GroupID uint64 `json:"group_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !containsGroup(u.Groups, req.GroupID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	token, err := a.signToken(newTokenClaims(u.ID, req.GroupID, u.AuthVersion))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token})
}

func (a *app) handleSetDefaultGroup(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	var req struct {
		GroupID uint64 `json:"group_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.GroupID == 0 {
		if _, err := a.db.Exec("UPDATE users SET default_group_id=NULL, updated_at=? WHERE id=?", nowSQL(), u.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "default_group_failed")
			return
		}
		u.DefaultGroupID = 0
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": u})
		return
	}
	if !containsGroup(u.Groups, req.GroupID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := a.db.Exec("UPDATE users SET default_group_id=?, updated_at=? WHERE id=?", req.GroupID, nowSQL(), u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "default_group_failed")
		return
	}
	u.DefaultGroupID = req.GroupID
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": u})
}

func (a *app) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "password_too_short")
		return
	}
	var oldHash string
	if err := a.db.QueryRow("SELECT password_hash FROM users WHERE id = ?", u.ID).Scan(&oldHash); err != nil {
		writeError(w, http.StatusInternalServerError, "user_not_found")
		return
	}
	if !verifyPassword(req.OldPassword, oldHash) {
		writeError(w, http.StatusUnauthorized, "invalid_password")
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_failed")
		return
	}
	_, err = a.db.Exec("UPDATE users SET password_hash = ?, must_change_password = 0, auth_version=auth_version+1, updated_at = ? WHERE id = ?", hash, nowSQL(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	members, _ := a.listMembers(groupID)
	date := queryDate(r, "date", time.Now().In(a.location))
	week, _ := a.currentWeekAt(groupID, date)
	var tasks []map[string]any
	if week != nil {
		if weekID, ok := week["id"].(uint64); ok {
			tasks, _ = a.weekTasks(groupID, weekID)
		}
	}
	learningConfig, _ := a.groupLearningConfig(groupID)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":            u,
		"members":         members,
		"current_week":    week,
		"current_tasks":   tasks,
		"learning_config": learningConfig,
	})
}

func (a *app) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	from := queryDate(r, "from", time.Now().In(a.location).AddDate(0, 0, -7))
	to := queryDate(r, "to", time.Now().In(a.location))
	rows, err := a.db.Query(`SELECT task_type, COUNT(*) FROM checkin_records WHERE group_id=? AND logical_date BETWEEN ? AND ? AND deleted_at IS NULL GROUP BY task_type`, groupID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "summary_failed")
		return
	}
	defer rows.Close()
	summary := map[string]int{}
	for rows.Next() {
		var k string
		var c int
		_ = rows.Scan(&k, &c)
		summary[k] = c
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "summary": summary})
}

func (a *app) handleDashboardMonthlyRanking(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	now := time.Now().In(a.location)
	if month == "" {
		month = now.Format("2006-01")
	}
	start, err := time.ParseInLocation("2006-01-02", month+"-01", a.location)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_month")
		return
	}
	end := start.AddDate(0, 1, -1)
	members, err := a.listMembers(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "members_failed")
		return
	}
	type score struct {
		MemberID    uint64         `json:"member_id"`
		UserID      uint64         `json:"user_id"`
		Username    string         `json:"username"`
		DisplayName string         `json:"display_name"`
		MemberName  string         `json:"member_name"`
		Counts      map[string]int `json:"counts"`
		Total       int            `json:"total"`
	}
	byUser := map[uint64]*score{}
	for _, member := range members {
		uid, _ := member["user_id"].(uint64)
		mid, _ := member["member_id"].(uint64)
		byUser[uid] = &score{
			MemberID:    mid,
			UserID:      uid,
			Username:    asString(member["username"]),
			DisplayName: asString(member["display_name"]),
			MemberName:  asString(member["member_name"]),
			Counts: map[string]int{
				"daily_devotion": 0,
				"weekly_book":    0,
				"weekly_video":   0,
				"weekly_verse":   0,
			},
		}
	}
	rows, err := a.db.Query(`SELECT user_id,task_type,COUNT(*)
		FROM checkin_records
		WHERE group_id=? AND logical_date BETWEEN ? AND ? AND deleted_at IS NULL
		  AND task_type IN ('daily_devotion','weekly_book','weekly_video','weekly_verse')
		GROUP BY user_id,task_type`, groupID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "monthly_ranking_failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var userID uint64
		var taskType string
		var count int
		if err := rows.Scan(&userID, &taskType, &count); err != nil {
			writeError(w, http.StatusInternalServerError, "monthly_ranking_failed")
			return
		}
		item, ok := byUser[userID]
		if !ok {
			continue
		}
		item.Counts[taskType] = count
		item.Total += count
	}
	items := make([]score, 0, len(byUser))
	for _, item := range byUser {
		items = append(items, *item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Total != items[j].Total {
			return items[i].Total > items[j].Total
		}
		if items[i].Counts["daily_devotion"] != items[j].Counts["daily_devotion"] {
			return items[i].Counts["daily_devotion"] > items[j].Counts["daily_devotion"]
		}
		if items[i].Counts["weekly_book"] != items[j].Counts["weekly_book"] {
			return items[i].Counts["weekly_book"] > items[j].Counts["weekly_book"]
		}
		if items[i].Counts["weekly_verse"] != items[j].Counts["weekly_verse"] {
			return items[i].Counts["weekly_verse"] > items[j].Counts["weekly_verse"]
		}
		return items[i].UserID < items[j].UserID
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"month": month,
		"from":  start.Format("2006-01-02"),
		"to":    end.Format("2006-01-02"),
		"items": items,
	})
}

func (a *app) handleMembers(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	members, err := a.listMembers(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "members_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (a *app) handleMemberCalendar(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	memberID, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().In(a.location).Format("2006-01")
	}
	start, err := time.ParseInLocation("2006-01-02", month+"-01", a.location)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_month")
		return
	}
	end := start.AddDate(0, 1, -1)
	rows, err := a.db.Query(`SELECT logical_date, task_type, part FROM checkin_records WHERE group_id=? AND user_id=? AND logical_date BETWEEN ? AND ? AND deleted_at IS NULL ORDER BY logical_date`, groupID, memberID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "calendar_failed")
		return
	}
	defer rows.Close()
	var items []map[string]string
	for rows.Next() {
		var d time.Time
		var t, p string
		_ = rows.Scan(&d, &t, &p)
		items = append(items, map[string]string{"date": d.Format("2006-01-02"), "task_type": t, "part": p})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *app) handleStudyWeeks(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	rows, err := a.db.Query(`SELECT id,start_date,end_date,title,verse_ref,recite_text,book_enabled,video_enabled,verse_enabled,outline_enabled FROM study_weeks WHERE group_id=? ORDER BY start_date`, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "weeks_failed")
		return
	}
	defer rows.Close()
	var weeks []map[string]any
	for rows.Next() {
		var id uint64
		var start, end time.Time
		var title, verse string
		var recite sql.NullString
		var book, video, verseEnabled, outline bool
		_ = rows.Scan(&id, &start, &end, &title, &verse, &recite, &book, &video, &verseEnabled, &outline)
		tasks, _ := a.weekTasks(groupID, id)
		readings, videos, outlineBinding := splitWeekTaskBindings(tasks)
		weeks = append(weeks, map[string]any{
			"id":              id,
			"start":           start.Format("2006-01-02"),
			"end":             end.Format("2006-01-02"),
			"title":           title,
			"verse_ref":       verse,
			"recite_text":     recite.String,
			"book_enabled":    book,
			"video_enabled":   video,
			"verse_enabled":   verseEnabled,
			"outline_enabled": outline,
			"readings":        readings,
			"videos":          videos,
			"outline":         outlineBinding,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"weeks": weeks})
}

func (a *app) handleCurrentStudyWeek(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	week, err := a.currentWeek(groupID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "week_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"week": week})
}

func (a *app) handleAdminCreateStudyWeek(w http.ResponseWriter, r *http.Request) {
	a.saveStudyWeek(w, r, 0)
}

func (a *app) handleAdminUpdateStudyWeek(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	a.saveStudyWeek(w, r, id)
}

func (a *app) handleAdminDeleteStudyWeek(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	weekID, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if weekID == 0 {
		writeError(w, http.StatusBadRequest, "week_id_required")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx_failed")
		return
	}
	defer tx.Rollback()
	if err := deleteStudyWeekTasksTx(tx, groupID, weekID); err != nil {
		writeError(w, http.StatusInternalServerError, "week_delete_failed")
		return
	}
	if _, err := tx.Exec(`UPDATE checkin_records SET week_id=NULL WHERE group_id=? AND week_id=?`, groupID, weekID); err != nil {
		writeError(w, http.StatusInternalServerError, "week_delete_failed")
		return
	}
	if _, err := tx.Exec(`DELETE FROM study_weeks WHERE id=? AND group_id=?`, weekID, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "week_delete_failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "week_delete_failed")
		return
	}
	a.audit(groupID, u.ID, "delete_study_week", "study_weeks", weekID, nil, nil, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) saveStudyWeek(w http.ResponseWriter, r *http.Request, id uint64) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var req studyWeekInput
	if !readJSON(w, r, &req) {
		return
	}
	if err := validateStudyWeekInput(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var overlap int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM study_weeks WHERE group_id=? AND id<>? AND NOT (end_date < ? OR start_date > ?)`, groupID, id, req.StartDate, req.EndDate).Scan(&overlap); err != nil {
		writeError(w, http.StatusInternalServerError, "week_validate_failed")
		return
	}
	if overlap > 0 {
		writeError(w, http.StatusConflict, "week_overlap")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx_failed")
		return
	}
	defer tx.Rollback()
	nowTime := time.Now().In(a.location)
	now := nowTime
	if id == 0 {
		res, err := tx.Exec(`INSERT INTO study_weeks (group_id,start_date,end_date,title,verse_ref,recite_text,book_enabled,video_enabled,verse_enabled,outline_enabled,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, groupID, req.StartDate, req.EndDate, req.Title, req.VerseRef, req.ReciteText, req.BookEnabled, req.VideoEnabled, req.VerseEnabled, req.OutlineEnabled, now, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "week_save_failed")
			return
		}
		id64, _ := res.LastInsertId()
		id = uint64(id64)
	} else {
		result, err := tx.Exec(`UPDATE study_weeks SET start_date=?,end_date=?,title=?,verse_ref=?,recite_text=?,book_enabled=?,video_enabled=?,verse_enabled=?,outline_enabled=?,updated_at=? WHERE id=? AND group_id=?`, req.StartDate, req.EndDate, req.Title, req.VerseRef, req.ReciteText, req.BookEnabled, req.VideoEnabled, req.VerseEnabled, req.OutlineEnabled, now, id, groupID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "week_save_failed")
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			writeError(w, http.StatusNotFound, "week_not_found")
			return
		}
	}
	if err := replaceStudyWeekTasksTx(tx, groupID, id, req, nowTime); err != nil {
		writeError(w, http.StatusInternalServerError, "week_task_save_failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "week_save_failed")
		return
	}
	a.audit(groupID, u.ID, "save_study_week", "study_weeks", id, nil, map[string]any{"title": req.Title}, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

func validateStudyWeekInput(req studyWeekInput) error {
	start, err := time.Parse("2006-01-02", strings.TrimSpace(req.StartDate))
	if err != nil {
		return errors.New("week_dates_required")
	}
	end, err := time.Parse("2006-01-02", strings.TrimSpace(req.EndDate))
	if err != nil || end.Before(start) {
		return errors.New("week_date_range_invalid")
	}
	if strings.TrimSpace(req.Title) == "" || len([]rune(strings.TrimSpace(req.Title))) > 200 {
		return errors.New("week_title_required")
	}
	for _, binding := range append(append([]weekTaskBinding{}, req.Readings...), req.Videos...) {
		if !validResourceURL(binding.URL) {
			return errors.New("invalid_resource_url")
		}
	}
	if !validResourceURL(req.Outline.URL) {
		return errors.New("invalid_resource_url")
	}
	return nil
}

func validateStudyWeekImport(weeks []studyWeekInput) error {
	type weekRange struct {
		start time.Time
		end   time.Time
	}
	ranges := make([]weekRange, 0, len(weeks))
	for _, week := range weeks {
		if err := validateStudyWeekInput(week); err != nil {
			return err
		}
		start, _ := time.Parse("2006-01-02", strings.TrimSpace(week.StartDate))
		end, _ := time.Parse("2006-01-02", strings.TrimSpace(week.EndDate))
		ranges = append(ranges, weekRange{start: start, end: end})
	}
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start.Before(ranges[j].start)
	})
	for index := 1; index < len(ranges); index++ {
		if !ranges[index].start.After(ranges[index-1].end) {
			return errors.New("week_overlap")
		}
	}
	return nil
}

func validResourceURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "//") || strings.HasPrefix(value, `\\`) {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return !strings.ContainsAny(value, "\\\r\n")
	}
	parsed, err := url.Parse(value)
	return err == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Host != "" &&
		!strings.ContainsAny(value, "\r\n")
}

func (a *app) handleCreateCheckin(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var req struct {
		TaskType    string `json:"task_type"`
		LogicalDate string `json:"logical_date"`
		Part        string `json:"part"`
		Detail      string `json:"detail"`
		Note        string `json:"note"`
		WeekID      uint64 `json:"week_id"`
		TaskID      uint64 `json:"task_id"`
		IsRetro     bool   `json:"is_retro"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.TaskType = strings.ToLower(strings.TrimSpace(req.TaskType))
	if !validCheckinTaskType(req.TaskType) {
		writeError(w, http.StatusBadRequest, "task_type_required")
		return
	}
	if req.LogicalDate == "" {
		req.LogicalDate = time.Now().In(a.location).Format("2006-01-02")
	}
	logicalDate, err := time.ParseInLocation("2006-01-02", req.LogicalDate, a.location)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_date")
		return
	}
	today := time.Now().In(a.location).Format("2006-01-02")
	if req.LogicalDate > today {
		writeError(w, http.StatusBadRequest, "future_checkin_not_allowed")
		return
	}
	switch req.TaskType {
	case "weekly_book", "weekly_video", "weekly_verse":
		if req.WeekID == 0 || req.TaskID == 0 {
			writeError(w, http.StatusBadRequest, "invalid_task")
			return
		}
		var canonicalTitle string
		err := a.db.QueryRow(`SELECT st.title
			FROM study_tasks st JOIN study_weeks sw ON sw.id=st.week_id AND sw.group_id=st.group_id
			WHERE st.id=? AND st.week_id=? AND st.group_id=? AND st.task_type=? AND st.enabled=1
			  AND ? BETWEEN sw.start_date AND sw.end_date`, req.TaskID, req.WeekID, groupID, req.TaskType, logicalDate.Format("2006-01-02")).Scan(&canonicalTitle)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_task")
			return
		}
		req.Detail = canonicalTitle
		if req.TaskType == "weekly_book" || req.TaskType == "weekly_video" {
			req.Part = canonicalTitle
		} else {
			req.Part = ""
		}
	case "daily_devotion":
		req.TaskID = 0
		req.Part = ""
		if req.WeekID > 0 {
			var weekExists int
			if err := a.db.QueryRow(`SELECT COUNT(1) FROM study_weeks WHERE id=? AND group_id=? AND ? BETWEEN start_date AND end_date`, req.WeekID, groupID, logicalDate.Format("2006-01-02")).Scan(&weekExists); err != nil || weekExists == 0 {
				writeError(w, http.StatusBadRequest, "invalid_week")
				return
			}
		}
	}
	now := nowSQL()
	res, err := a.db.Exec(`INSERT INTO checkin_records (group_id,user_id,task_id,week_id,logical_date,checkin_time,task_type,status,is_retro,detail,note,part,source,created_by,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, groupID, u.ID, nullableID(req.TaskID), nullableID(req.WeekID), req.LogicalDate, now, req.TaskType, "done", req.LogicalDate != today, truncate(req.Detail, 1000), truncate(req.Note, 4000), truncate(req.Part, 64), "web", u.ID, now, now)
	if err != nil {
		writeError(w, http.StatusConflict, "checkin_save_failed")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *app) handleDeleteOwnCheckin(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if id == 0 {
		writeError(w, http.StatusBadRequest, "checkin_id_required")
		return
	}
	result, err := a.db.Exec(`UPDATE checkin_records SET deleted_at=?, active_key=id, updated_at=? WHERE id=? AND group_id=? AND user_id=? AND deleted_at IS NULL`, nowSQL(), nowSQL(), id, groupID, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed")
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "checkin_not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func validCheckinTaskType(taskType string) bool {
	switch taskType {
	case "daily_devotion", "weekly_book", "weekly_video", "weekly_verse":
		return true
	default:
		return false
	}
}

func (a *app) handleAdminDeleteCheckin(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	_, err := a.db.Exec(`UPDATE checkin_records SET deleted_at=?, active_key=id, updated_at=? WHERE id=? AND group_id=? AND deleted_at IS NULL`, nowSQL(), nowSQL(), id, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed")
		return
	}
	a.audit(groupID, u.ID, "delete_checkin", "checkin_records", id, nil, nil, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleListCheckins(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	from := queryDate(r, "from", time.Now().In(a.location).AddDate(0, 0, -30))
	to := queryDate(r, "to", time.Now().In(a.location))
	userID, _ := strconv.ParseUint(r.URL.Query().Get("user_id"), 10, 64)
	canViewDetails := u.IsSuperAdmin || hasRole(u.Roles, roleGroupAdmin) || hasRole(u.Roles, roleGroupLeader)
	if userID > 0 && userID != u.ID && !canViewDetails {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	limit := clampInt(queryInt(r, "page_size", 50), 1, 200)
	args := []any{groupID, from, to}
	where := "group_id=? AND logical_date BETWEEN ? AND ? AND deleted_at IS NULL"
	if userID > 0 {
		where += " AND user_id=?"
		args = append(args, userID)
	}
	args = append(args, limit)
	rows, err := a.db.Query(`SELECT id,user_id,task_id,logical_date,checkin_time,task_type,part,detail,note FROM checkin_records WHERE `+where+` ORDER BY logical_date DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checkins_failed")
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, uid uint64
		var taskID sql.NullInt64
		var d, ts time.Time
		var tt, part, detail string
		var note sql.NullString
		if err := rows.Scan(&id, &uid, &taskID, &d, &ts, &tt, &part, &detail, &note); err != nil {
			writeError(w, http.StatusInternalServerError, "checkins_failed")
			return
		}
		item := map[string]any{"id": id, "user_id": uid, "task_id": nullableUint64(taskID), "logical_date": d.Format("2006-01-02"), "checkin_time": ts.Format(time.RFC3339), "task_type": tt, "part": part, "detail": detail, "note": note.String}
		if uid != u.ID && !canViewDetails {
			item["id"] = uint64(0)
			item["detail"] = ""
			item["note"] = ""
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "checkins_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *app) handleListAssets(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	rows, err := a.db.Query(`SELECT id,category,title,original_name,mime_type,file_size FROM assets WHERE group_id=? ORDER BY category,title LIMIT 200`, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "assets_failed")
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, size uint64
		var category, title, original, mt string
		_ = rows.Scan(&id, &category, &title, &original, &mt, &size)
		items = append(items, map[string]any{"id": id, "category": category, "title": title, "original_name": original, "mime_type": mt, "file_size": size})
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": items})
}

func (a *app) handleDownloadAsset(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	abs, original, mt, err := a.resolveAssetPath(groupID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found")
		return
	}
	if mt == "" {
		mt = mime.TypeByExtension(filepath.Ext(original))
	}
	w.Header().Set("Content-Type", mt)
	http.ServeFile(w, r, abs)
}

func (a *app) handleDownloadAssetRange(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	abs, original, _, err := a.resolveAssetPath(groupID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found")
		return
	}
	pages, err := normalizePageRange(r.URL.Query().Get("pages"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pages")
		return
	}
	if strings.ToLower(filepath.Ext(original)) != ".pdf" {
		writeError(w, http.StatusBadRequest, "asset_not_pdf")
		return
	}
	if err := servePDFRange(w, abs, original, pages); err != nil {
		writeError(w, http.StatusInternalServerError, "pdf_range_failed")
	}
}

func (a *app) handleStaticPDFRange(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	pages, err := normalizePageRange(r.URL.Query().Get("pages"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pages")
		return
	}
	src := strings.TrimSpace(r.URL.Query().Get("path"))
	abs, original, err := resolveFileInRoot(a.contentRoot, src)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pdf_path")
		return
	}
	if strings.ToLower(filepath.Ext(original)) != ".pdf" {
		writeError(w, http.StatusBadRequest, "asset_not_pdf")
		return
	}
	if err := servePDFRange(w, abs, original, pages); err != nil {
		writeError(w, http.StatusInternalServerError, "pdf_range_failed")
	}
}

func (a *app) resolveAssetPath(groupID, id uint64) (string, string, string, error) {
	var storagePath, original, mt string
	if err := a.db.QueryRow(`SELECT storage_path, original_name, mime_type FROM assets WHERE id=? AND group_id=?`, id, groupID).Scan(&storagePath, &original, &mt); err != nil {
		return "", "", "", err
	}
	abs, _, err := resolveFileInRoot(a.assetsRoot, storagePath)
	if err != nil {
		return "", "", "", err
	}
	return abs, original, mt, nil
}

func resolveFileInRoot(root, path string) (string, string, error) {
	if strings.TrimSpace(root) == "" {
		return "", "", errors.New("empty_root")
	}
	decoded, err := url.PathUnescape(strings.TrimSpace(path))
	if err == nil && decoded != "" {
		path = decoded
	}
	clean := filepath.Clean(strings.TrimPrefix(path, "/"))
	full := filepath.Join(root, clean)
	absRoot, _ := filepath.Abs(root)
	abs, _ := filepath.Abs(full)
	if abs != absRoot && !strings.HasPrefix(abs, absRoot+string(os.PathSeparator)) {
		return "", "", errors.New("invalid_path")
	}
	return abs, filepath.Base(clean), nil
}

func normalizePageRange(input string) (string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", errors.New("empty_pages")
	}
	parts := strings.SplitN(raw, "-", 2)
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || start < 1 {
		return "", errors.New("invalid_start")
	}
	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || end < start {
			return "", errors.New("invalid_end")
		}
	}
	return fmt.Sprintf("%d-%d", start, end), nil
}

func servePDFRange(w http.ResponseWriter, srcPath, original, pages string) error {
	tmp, err := os.CreateTemp("", "agp-pdf-range-*.pdf")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if err := pdfapi.TrimFile(srcPath, tmp.Name(), []string{pages}, nil); err != nil {
		return err
	}
	file, err := os.Open(tmp.Name())
	if err != nil {
		return err
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(original)))
	_, err = io.Copy(w, file)
	return err
}

func (a *app) handleAdminUploadAsset(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	category, err := normalizeAssetCategory(r.FormValue("category"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_asset_category")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required")
		return
	}
	defer file.Close()
	safeName := sanitizeUploadName(header.Filename)
	if safeName == "" {
		writeError(w, http.StatusBadRequest, "invalid_filename")
		return
	}
	ext := strings.ToLower(filepath.Ext(safeName))
	if !allowedUploadExtension(ext) {
		writeError(w, http.StatusBadRequest, "unsupported_file_type")
		return
	}
	buffered := bufio.NewReader(file)
	head, _ := buffered.Peek(512)
	detectedType := http.DetectContentType(head)
	if !allowedUploadContentType(ext, detectedType) {
		writeError(w, http.StatusBadRequest, "unsupported_file_type")
		return
	}
	relativeDir := filepath.Join(strconv.FormatUint(groupID, 10), category)
	absoluteDir := filepath.Join(a.assetsRoot, relativeDir)
	if err := os.MkdirAll(absoluteDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "asset_dir_failed")
		return
	}
	relativePath := filepath.Join(relativeDir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), safeName))
	absolutePath := filepath.Join(a.assetsRoot, relativePath)
	dst, err := os.CreateTemp(absoluteDir, ".agp-upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_write_failed")
		return
	}
	tempPath := dst.Name()
	defer os.Remove(tempPath)
	hasher := sha256.New()
	size, copyErr := io.Copy(dst, io.TeeReader(buffered, hasher))
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		writeError(w, http.StatusInternalServerError, "asset_write_failed")
		return
	}
	if size <= 0 || size > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large")
		return
	}
	if err := os.Rename(tempPath, absolutePath); err != nil {
		writeError(w, http.StatusInternalServerError, "asset_write_failed")
		return
	}
	title := truncate(strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename)), 255)
	mt := mime.TypeByExtension(ext)
	if mt == "" {
		mt = detectedType
	}
	now := nowSQL()
	res, err := a.db.Exec(`INSERT INTO assets (group_id,category,title,original_name,storage_path,mime_type,file_size,checksum_sha256,created_by,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		groupID, category, title, header.Filename, filepath.ToSlash(relativePath), mt, size, hex.EncodeToString(hasher.Sum(nil)), u.ID, now, now)
	if err != nil {
		_ = os.Remove(absolutePath)
		writeError(w, http.StatusInternalServerError, "asset_save_failed")
		return
	}
	id, _ := res.LastInsertId()
	a.audit(groupID, u.ID, "upload_asset", "assets", uint64(id), nil, map[string]any{"title": title, "category": category}, r)
	writeJSON(w, http.StatusCreated, map[string]any{
		"asset": map[string]any{
			"id":            id,
			"category":      category,
			"title":         title,
			"original_name": header.Filename,
			"url":           fmt.Sprintf("/api/assets/%d/download", id),
		},
	})
}

func (a *app) handleAdminResourceLibrary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	sections, err := a.resourceLibrarySections(groupID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resource_library_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sections": sections})
}

func (a *app) handleResourceLibrary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	sections, err := a.resourceLibrarySections(groupID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resource_library_failed")
		return
	}
	for _, section := range sections {
		delete(section, "managed_root")
	}
	writeJSON(w, http.StatusOK, map[string]any{"sections": sections})
}

func (a *app) resourceLibrarySections(groupID uint64, includeHidden bool) ([]map[string]any, error) {
	sections := []map[string]any{
		a.scanStaticLibrarySection("markdown", "Markdown 读物", "", "/", []string{".md"}),
		a.scanStaticLibrarySection("book", "PDF 读物", "Book", "/Book", []string{".pdf"}),
		a.scanStaticLibrarySection("passage", "课程读物", "Passage", "/Passage", []string{".pdf"}),
		a.scanStaticLibrarySection("handout", "讲义 PDF", "PPT", "/PPT", []string{".pdf"}),
		a.scanStaticLibrarySection("mentor", "导师资料", "Mentor", "/Mentor", []string{".pdf", ".md", ".png", ".jpg", ".jpeg", ".webp", ".mp4"}),
	}
	uploaded, err := a.uploadedLibrarySections(groupID)
	if err != nil {
		return nil, err
	}
	sections = append(sections, uploaded...)
	visibility, err := a.resourceLibraryVisibility(groupID)
	if err != nil {
		return nil, err
	}
	for _, section := range sections {
		items, _ := section["items"].([]map[string]any)
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			key := resourceLibraryKey(item)
			item["resource_key"] = key
			defaultVisible := asString(item["source"]) == "uploaded"
			visible, configured := visibility[key]
			if !configured {
				visible = defaultVisible
			}
			item["visible_in_library"] = visible
			if includeHidden || visible {
				filtered = append(filtered, item)
			}
		}
		section["items"] = filtered
		section["count"] = len(filtered)
	}
	return sections, nil
}

func resourceLibraryKey(item map[string]any) string {
	id, _ := item["id"].(uint64)
	if id > 0 {
		return fmt.Sprintf("asset:%d", id)
	}
	category := strings.ToLower(strings.TrimSpace(asString(item["category"])))
	relative := strings.TrimSpace(asString(item["relative_path"]))
	if relative != "" {
		return "static:" + category + ":" + filepath.ToSlash(relative)
	}
	return "url:" + strings.TrimSpace(asString(item["url"]))
}

func (a *app) resourceLibraryVisibility(groupID uint64) (map[string]bool, error) {
	rows, err := a.db.Query(`SELECT resource_key,visible FROM resource_library_visibility WHERE group_id=?`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var key string
		var visible bool
		if err := rows.Scan(&key, &visible); err != nil {
			return nil, err
		}
		result[key] = visible
	}
	return result, rows.Err()
}

func (a *app) handleAdminResourceLibraryVisibility(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var req struct {
		ResourceKey string `json:"resource_key"`
		Visible     bool   `json:"visible"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	req.ResourceKey = strings.TrimSpace(req.ResourceKey)
	if req.ResourceKey == "" || len(req.ResourceKey) > 512 {
		writeError(w, http.StatusBadRequest, "invalid_resource_key")
		return
	}
	sections, err := a.resourceLibrarySections(groupID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resource_library_failed")
		return
	}
	found := false
	for _, section := range sections {
		items, _ := section["items"].([]map[string]any)
		for _, item := range items {
			if asString(item["resource_key"]) == req.ResourceKey {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "resource_not_found")
		return
	}
	now := nowSQL()
	_, err = a.db.Exec(`INSERT INTO resource_library_visibility (group_id,resource_key,visible,updated_by,created_at,updated_at)
		VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE visible=VALUES(visible),updated_by=VALUES(updated_by),updated_at=VALUES(updated_at)`,
		groupID, req.ResourceKey, req.Visible, u.ID, now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resource_visibility_save_failed")
		return
	}
	a.audit(groupID, u.ID, "set_resource_library_visibility", "resource", 0, nil, map[string]any{"resource_key": req.ResourceKey, "visible": req.Visible}, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "resource_key": req.ResourceKey, "visible": req.Visible})
}

func managedResourceRoot(contentRoot, key string) (string, string, error) {
	var directory string
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "book":
		directory = "Book"
	case "passage":
		directory = "Passage"
	case "ppt", "handout":
		directory = "PPT"
	case "mentor":
		directory = "Mentor"
	default:
		return "", "", errors.New("unsupported_resource_root")
	}
	return filepath.Join(contentRoot, directory), directory, nil
}

func cleanResourceFolderPath(value string, allowEmpty bool) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	if value == "" {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("folder_required")
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("invalid_folder_path")
	}
	return clean, nil
}

func cleanResourceFolderName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || name == "." || name == ".." || len([]rune(name)) > 100 || strings.ContainsAny(name, "/\\\x00") {
		return "", errors.New("invalid_folder_name")
	}
	return name, nil
}

func (a *app) handleAdminCreateResourceFolder(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var req struct {
		Root   string `json:"root"`
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, rootName, err := managedResourceRoot(a.contentRoot, req.Root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	parent, err := cleanResourceFolderPath(req.Parent, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := cleanResourceFolderName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	target := filepath.Join(root, parent, name)
	if err := os.MkdirAll(target, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "folder_create_failed")
		return
	}
	a.audit(groupID, u.ID, "create_resource_folder", "resource_folder", 0, nil, map[string]any{"root": rootName, "path": filepath.ToSlash(filepath.Join(parent, name))}, r)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "root": rootName, "path": filepath.ToSlash(filepath.Join(parent, name))})
}

func (a *app) handleAdminRenameResourceFolder(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var req struct {
		Root string `json:"root"`
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	root, rootName, err := managedResourceRoot(a.contentRoot, req.Root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	oldPath, err := cleanResourceFolderPath(req.Path, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := cleanResourceFolderName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	source := filepath.Join(root, oldPath)
	targetPath := filepath.Join(filepath.Dir(oldPath), name)
	target := filepath.Join(root, targetPath)
	if _, err := os.Stat(source); err != nil {
		writeError(w, http.StatusNotFound, "folder_not_found")
		return
	}
	if _, err := os.Stat(target); err == nil {
		writeError(w, http.StatusConflict, "folder_exists")
		return
	}
	if err := os.Rename(source, target); err != nil {
		writeError(w, http.StatusInternalServerError, "folder_rename_failed")
		return
	}
	a.audit(groupID, u.ID, "rename_resource_folder", "resource_folder", 0, map[string]any{"root": rootName, "path": filepath.ToSlash(oldPath)}, map[string]any{"root": rootName, "path": filepath.ToSlash(targetPath)}, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "root": rootName, "path": filepath.ToSlash(targetPath)})
}

func (a *app) handleAdminLearningConfig(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	settings, err := a.groupLearningConfig(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "learning_config_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (a *app) handleAdminSaveLearningConfig(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var settings map[string]any
	if !readJSON(w, r, &settings) {
		return
	}
	if settings == nil {
		settings = map[string]any{}
	}
	if err := a.upsertGroupLearningConfig(groupID, settings); err != nil {
		writeError(w, http.StatusInternalServerError, "learning_config_save_failed")
		return
	}
	a.audit(groupID, u.ID, "save_learning_config", "group_settings", groupID, nil, map[string]any{"keys": len(settings)}, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": settings})
}

func (a *app) handleAdminCreateAsset(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var req struct {
		Category    string `json:"category"`
		Title       string `json:"title"`
		StoragePath string `json:"storage_path"`
		MimeType    string `json:"mime_type"`
		FileSize    uint64 `json:"file_size"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	now := nowSQL()
	res, err := a.db.Exec(`INSERT INTO assets (group_id,category,title,original_name,storage_path,mime_type,file_size,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, groupID, req.Category, req.Title, filepath.Base(req.StoragePath), req.StoragePath, req.MimeType, req.FileSize, u.ID, now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "asset_save_failed")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *app) handleAdminMembers(w http.ResponseWriter, r *http.Request) {
	a.handleMembers(w, r)
}

func (a *app) handleAdminCreateMember(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var req struct {
		CreateUser  bool   `json:"create_user"`
		UserID      uint64 `json:"user_id"`
		DisplayName string `json:"display_name"`
		Username    string `json:"username"`
		NamePinyin  string `json:"name_pinyin"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx_failed")
		return
	}
	defer tx.Rollback()
	userID := req.UserID
	if req.CreateUser {
		username := normalizeUsername(firstNonEmpty(req.Username, req.NamePinyin, req.DisplayName))
		if username == "" || req.DisplayName == "" {
			writeError(w, http.StatusBadRequest, "username_display_name_required")
			return
		}
		hash, err := a.groupDefaultPasswordHashTx(tx, groupID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "group_default_password_missing")
			return
		}
		now := nowSQL()
		res, err := tx.Exec(`INSERT INTO users (username,display_name,name_pinyin,password_hash,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`, username, req.DisplayName, firstNonEmpty(req.NamePinyin, username), hash, u.ID, now, now)
		if err != nil {
			writeError(w, http.StatusConflict, "user_create_failed")
			return
		}
		id, _ := res.LastInsertId()
		userID = uint64(id)
	}
	if userID == 0 {
		writeError(w, http.StatusBadRequest, "user_id_required")
		return
	}
	if err := addMemberTx(tx, groupID, userID, req.DisplayName, u.ID); err != nil {
		writeError(w, http.StatusConflict, "member_add_failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "member_save_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user_id": userID})
}

func (a *app) handleAdminRemoveMember(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	memberID, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var targetUserID uint64
	var isSuper bool
	var leaderCount int
	if err := a.db.QueryRow(`SELECT u.id,u.is_super_admin
		FROM group_members m JOIN users u ON u.id=m.user_id
                WHERE m.id=? AND m.group_id=? AND m.status=1`, memberID, groupID).Scan(&targetUserID, &isSuper); err != nil {
		writeError(w, http.StatusNotFound, "member_not_found")
		return
	}
	if err := a.db.QueryRow("SELECT COUNT(1) FROM user_group_roles WHERE group_id=? AND user_id=? AND role=?", groupID, targetUserID, roleGroupLeader).Scan(&leaderCount); err != nil {
		writeError(w, http.StatusInternalServerError, "member_role_check_failed")
		return
	}
	if targetUserID == u.ID {
		writeError(w, http.StatusBadRequest, "cannot_remove_self")
		return
	}
	if isSuper && !u.IsSuperAdmin {
		writeError(w, http.StatusForbidden, "cannot_remove_super_admin")
		return
	}
	if leaderCount > 0 {
		writeError(w, http.StatusForbidden, "cannot_remove_group_leader")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx_failed")
		return
	}
	defer tx.Rollback()
	now := nowSQL()
	if _, err := tx.Exec("UPDATE group_members SET status=0, updated_at=? WHERE id=? AND group_id=?", now, memberID, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "member_remove_failed")
		return
	}
	if _, err := tx.Exec("DELETE FROM user_group_roles WHERE group_id=? AND user_id=?", groupID, targetUserID); err != nil {
		writeError(w, http.StatusInternalServerError, "member_role_remove_failed")
		return
	}
	if _, err := tx.Exec("UPDATE users SET default_group_id=NULL, updated_at=? WHERE id=? AND default_group_id=?", now, targetUserID, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "default_group_update_failed")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "member_remove_failed")
		return
	}
	a.audit(groupID, u.ID, "remove_member", "group_members", memberID, nil, map[string]any{"user_id": targetUserID}, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleAdminSetGroupDefaultPassword(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	affected, err := a.setGroupDefaultPassword(groupID, req.Password, false, u.ID, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "affected_users": affected, "message": "多小组成员、组长和超级管理员账号不会被本组默认密码覆盖。"})
}

func (a *app) handleGrantGroupAdmin(w http.ResponseWriter, r *http.Request) {
	a.setRole(w, r, roleGroupAdmin, true)
}

func (a *app) handleRevokeGroupAdmin(w http.ResponseWriter, r *http.Request) {
	a.setRole(w, r, roleGroupAdmin, false)
}

func (a *app) setRole(w http.ResponseWriter, r *http.Request, role string, grant bool) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	memberID, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var targetUserID uint64
	var isSuper bool
	var leaderCount int
	if err := a.db.QueryRow(`SELECT u.id,u.is_super_admin
                FROM group_members m JOIN users u ON u.id=m.user_id
                WHERE m.id=? AND m.group_id=? AND m.status=1`, memberID, groupID).Scan(&targetUserID, &isSuper); err != nil {
		writeError(w, http.StatusNotFound, "member_not_found")
		return
	}
	if err := a.db.QueryRow("SELECT COUNT(1) FROM user_group_roles WHERE group_id=? AND user_id=? AND role=?", groupID, targetUserID, roleGroupLeader).Scan(&leaderCount); err != nil {
		writeError(w, http.StatusInternalServerError, "member_role_check_failed")
		return
	}
	if targetUserID == u.ID {
		writeError(w, http.StatusBadRequest, "cannot_manage_self")
		return
	}
	if isSuper {
		writeError(w, http.StatusForbidden, "cannot_manage_super_admin")
		return
	}
	if leaderCount > 0 {
		writeError(w, http.StatusForbidden, "cannot_manage_group_leader")
		return
	}
	if grant {
		_, _ = a.db.Exec("INSERT IGNORE INTO user_group_roles (group_id,user_id,role,created_at) VALUES (?,?,?,?)", groupID, targetUserID, role, nowSQL())
	} else {
		_, _ = a.db.Exec("DELETE FROM user_group_roles WHERE group_id=? AND user_id=? AND role=?", groupID, targetUserID, role)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	rows, err := a.db.Query(`SELECT id,actor_user_id,action,target_type,target_id,created_at FROM audit_logs WHERE group_id=? ORDER BY id DESC LIMIT 100`, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "audit_failed")
		return
	}
	defer rows.Close()
	var items []map[string]any
	for rows.Next() {
		var id, actor, target uint64
		var action, targetType string
		var created time.Time
		_ = rows.Scan(&id, &actor, &action, &targetType, &target, &created)
		items = append(items, map[string]any{"id": id, "actor_user_id": actor, "action": action, "target_type": targetType, "target_id": target, "created_at": created.Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type localBackupMember struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	NamePinyin  string   `json:"name_pinyin"`
	Roles       []string `json:"roles"`
}

type localBackupCheckin struct {
	Username    string `json:"username"`
	LogicalDate string `json:"logical_date"`
	CheckinTime string `json:"checkin_time"`
	TaskType    string `json:"task_type"`
	Part        string `json:"part"`
	Detail      string `json:"detail"`
	Note        string `json:"note"`
	IsRetro     bool   `json:"is_retro"`
}

type localBackupFeedback struct {
	Username  string `json:"username"`
	Name      string `json:"name"`
	Contact   string `json:"contact"`
	Message   string `json:"message"`
	Page      string `json:"page"`
	UserAgent string `json:"user_agent"`
	CreatedAt string `json:"created_at"`
}

type localBackupAsset struct {
	Category     string `json:"category"`
	Title        string `json:"title"`
	OriginalName string `json:"original_name"`
	StoragePath  string `json:"storage_path"`
	MimeType     string `json:"mime_type"`
	FileSize     uint64 `json:"file_size"`
}

type localBackupPayload struct {
	Version    int                   `json:"version"`
	ExportedAt string                `json:"exported_at"`
	Group      map[string]any        `json:"group"`
	Settings   map[string]any        `json:"settings"`
	Members    []localBackupMember   `json:"members"`
	Weeks      []studyWeekInput      `json:"weeks"`
	Checkins   []localBackupCheckin  `json:"checkins"`
	Feedbacks  []localBackupFeedback `json:"feedbacks"`
	Assets     []localBackupAsset    `json:"assets"`
}

func validateLocalBackupPayload(payload localBackupPayload) error {
	if payload.Version != 1 {
		return errors.New("backup_version_unsupported")
	}
	if len(payload.Settings) == 0 && len(payload.Members) == 0 && len(payload.Weeks) == 0 && len(payload.Checkins) == 0 && len(payload.Feedbacks) == 0 {
		return errors.New("backup_empty")
	}
	if len(payload.Members) > 5000 || len(payload.Weeks) > 1000 || len(payload.Checkins) > 500000 || len(payload.Feedbacks) > 100000 {
		return errors.New("backup_too_large")
	}
	usernames := make(map[string]struct{}, len(payload.Members))
	for _, member := range payload.Members {
		username := normalizeUsername(member.Username)
		if username == "" {
			return errors.New("backup_member_invalid")
		}
		if _, exists := usernames[username]; exists {
			return errors.New("backup_member_duplicate")
		}
		usernames[username] = struct{}{}
	}
	for index, week := range payload.Weeks {
		if err := validateStudyWeekInput(week); err != nil {
			return err
		}
		for previous := 0; previous < index; previous++ {
			other := payload.Weeks[previous]
			if week.StartDate <= other.EndDate && week.EndDate >= other.StartDate {
				return errors.New("week_overlap")
			}
		}
	}
	for _, checkin := range payload.Checkins {
		if normalizeUsername(checkin.Username) == "" {
			return errors.New("backup_checkin_member_invalid")
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(checkin.LogicalDate)); err != nil {
			return errors.New("backup_checkin_date_invalid")
		}
		if !validImportedCheckinTaskType(strings.ToLower(strings.TrimSpace(checkin.TaskType))) {
			return errors.New("backup_checkin_type_invalid")
		}
	}
	return nil
}

func validImportedCheckinTaskType(taskType string) bool {
	switch taskType {
	case "daily_devotion", "weekly_book", "weekly_video", "weekly_verse", "reflection", "recite_exam":
		return true
	default:
		return false
	}
}

func writeAttachmentHeaders(w http.ResponseWriter, filename, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
}

func groupExportPrefix(groupID uint64) string {
	if groupID == 0 {
		return "group"
	}
	return fmt.Sprintf("group-%d", groupID)
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func parseFlexibleBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return fallback
	case "1", "true", "yes", "y", "是":
		return true
	case "0", "false", "no", "n", "否":
		return false
	default:
		return fallback
	}
}

func parseTimeOrNow(value string, fallback time.Time) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04:05.000"} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return fallback
}

func excelCell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func weekKey(startDate, endDate string) string {
	return strings.TrimSpace(startDate) + "|" + strings.TrimSpace(endDate)
}

func deleteAllStudyWeeksTx(tx *sql.Tx, groupID uint64) error {
	if _, err := tx.Exec(`UPDATE checkin_records SET task_id=NULL,week_id=NULL WHERE group_id=?`, groupID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE ta FROM task_assets ta
		JOIN study_tasks st ON st.id=ta.task_id
		WHERE st.group_id=?`, groupID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM study_tasks WHERE group_id=?`, groupID); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM study_weeks WHERE group_id=?`, groupID)
	return err
}

func upsertGroupLearningConfigTx(tx *sql.Tx, groupID uint64, settings map[string]any) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	now := nowSQL()
	_, err = tx.Exec(`INSERT INTO group_settings (group_id,settings,created_at,updated_at)
		VALUES (?,?,?,?)
		ON DUPLICATE KEY UPDATE settings=VALUES(settings), updated_at=VALUES(updated_at)`,
		groupID, string(payload), now, now)
	return err
}

func (a *app) listStudyWeekInputs(groupID uint64) ([]studyWeekInput, error) {
	rows, err := a.db.Query(`SELECT id,start_date,end_date,title,verse_ref,recite_text,book_enabled,video_enabled,verse_enabled,outline_enabled
		FROM study_weeks WHERE group_id=? ORDER BY start_date,id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var weeks []studyWeekInput
	for rows.Next() {
		var id uint64
		var start, end time.Time
		var title, verse string
		var recite sql.NullString
		var bookEnabled, videoEnabled, verseEnabled, outlineEnabled bool
		if err := rows.Scan(&id, &start, &end, &title, &verse, &recite, &bookEnabled, &videoEnabled, &verseEnabled, &outlineEnabled); err != nil {
			return nil, err
		}
		tasks, err := a.weekTasks(groupID, id)
		if err != nil {
			return nil, err
		}
		readings, videos, outline := splitWeekTaskBindings(tasks)
		weeks = append(weeks, studyWeekInput{
			StartDate:      start.Format("2006-01-02"),
			EndDate:        end.Format("2006-01-02"),
			Title:          title,
			VerseRef:       verse,
			ReciteText:     recite.String,
			BookEnabled:    bookEnabled,
			VideoEnabled:   videoEnabled,
			VerseEnabled:   verseEnabled,
			OutlineEnabled: outlineEnabled,
			Readings:       readings,
			Videos:         videos,
			Outline:        outline,
		})
	}
	return weeks, rows.Err()
}

func (a *app) handleAdminExportCheckinsCSV(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	rows, err := a.db.Query(`SELECT c.id,c.logical_date,c.checkin_time,c.task_type,c.part,c.detail,COALESCE(c.note,''),c.is_retro,u.username,COALESCE(m.member_name,u.display_name)
		FROM checkin_records c
		JOIN users u ON u.id=c.user_id
		LEFT JOIN group_members m ON m.group_id=c.group_id AND m.user_id=c.user_id AND m.status=1
		WHERE c.group_id=? AND c.deleted_at IS NULL
		ORDER BY c.logical_date DESC,c.id DESC`, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_checkins_failed")
		return
	}
	defer rows.Close()
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{"记录ID", "打卡日期", "打卡时间", "账号", "成员姓名", "任务类型", "子项", "详情", "备注", "是否补卡"})
	for rows.Next() {
		var id uint64
		var logicalDate, taskType, part, detail, note, username, memberName string
		var checkinTime time.Time
		var isRetro bool
		if err := rows.Scan(&id, &logicalDate, &checkinTime, &taskType, &part, &detail, &note, &isRetro, &username, &memberName); err != nil {
			writeError(w, http.StatusInternalServerError, "export_checkins_failed")
			return
		}
		_ = writer.Write([]string{
			strconv.FormatUint(id, 10),
			logicalDate,
			checkinTime.In(a.location).Format("2006-01-02 15:04:05"),
			username,
			memberName,
			taskType,
			part,
			detail,
			note,
			boolString(isRetro),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		writeError(w, http.StatusInternalServerError, "export_checkins_failed")
		return
	}
	writeAttachmentHeaders(w, fmt.Sprintf("%s-checkins-detail.csv", groupExportPrefix(groupID)), "text/csv; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (a *app) handleAdminExportDailySummaryCSV(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var activeMembers int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM group_members WHERE group_id=? AND status=1`, groupID).Scan(&activeMembers); err != nil {
		writeError(w, http.StatusInternalServerError, "export_daily_summary_failed")
		return
	}
	rows, err := a.db.Query(`SELECT logical_date,
		COUNT(*) AS total_checkins,
		COUNT(DISTINCT user_id) AS checked_members,
		SUM(CASE WHEN task_type='daily_devotion' THEN 1 ELSE 0 END) AS devotion_count,
		SUM(CASE WHEN task_type='weekly_book' THEN 1 ELSE 0 END) AS book_count,
		SUM(CASE WHEN task_type='weekly_video' THEN 1 ELSE 0 END) AS video_count,
		SUM(CASE WHEN task_type='weekly_verse' THEN 1 ELSE 0 END) AS verse_count
		FROM checkin_records
		WHERE group_id=? AND deleted_at IS NULL
		GROUP BY logical_date
		ORDER BY logical_date DESC`, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_daily_summary_failed")
		return
	}
	defer rows.Close()
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{"日期", "组内活跃人数", "当日打卡人数", "总打卡数", "灵修数", "读物数", "视频数", "背经数", "完成率"})
	for rows.Next() {
		var logicalDate string
		var totalCheckins, checkedMembers, devotionCount, bookCount, videoCount, verseCount int
		if err := rows.Scan(&logicalDate, &totalCheckins, &checkedMembers, &devotionCount, &bookCount, &videoCount, &verseCount); err != nil {
			writeError(w, http.StatusInternalServerError, "export_daily_summary_failed")
			return
		}
		rate := 0.0
		if activeMembers > 0 {
			rate = float64(totalCheckins) / float64(activeMembers*4) * 100
		}
		_ = writer.Write([]string{
			logicalDate,
			strconv.Itoa(activeMembers),
			strconv.Itoa(checkedMembers),
			strconv.Itoa(totalCheckins),
			strconv.Itoa(devotionCount),
			strconv.Itoa(bookCount),
			strconv.Itoa(videoCount),
			strconv.Itoa(verseCount),
			fmt.Sprintf("%.2f%%", rate),
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		writeError(w, http.StatusInternalServerError, "export_daily_summary_failed")
		return
	}
	writeAttachmentHeaders(w, fmt.Sprintf("%s-daily-summary.csv", groupExportPrefix(groupID)), "text/csv; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (a *app) handleAdminExportStudyWeeksExcel(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	weeks, err := a.listStudyWeekInputs(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_study_weeks_failed")
		return
	}
	file := excelize.NewFile()
	file.SetSheetName("Sheet1", "Weeks")
	_ = file.SetSheetRow("Weeks", "A1", &[]string{"开始日期", "结束日期", "标题", "背经经文", "默写原文", "显示读物", "显示视频", "显示背经", "显示提纲"})
	_, _ = file.NewSheet("Readings")
	_ = file.SetSheetRow("Readings", "A1", &[]string{"开始日期", "结束日期", "排序", "标题", "URL", "资产ID"})
	_, _ = file.NewSheet("Videos")
	_ = file.SetSheetRow("Videos", "A1", &[]string{"开始日期", "结束日期", "排序", "标题", "URL", "资产ID"})
	_, _ = file.NewSheet("Outlines")
	_ = file.SetSheetRow("Outlines", "A1", &[]string{"开始日期", "结束日期", "标题", "URL", "类型", "资产ID"})
	weekRow, readingRow, videoRow, outlineRow := 2, 2, 2, 2
	for _, week := range weeks {
		_ = file.SetSheetRow("Weeks", fmt.Sprintf("A%d", weekRow), &[]any{
			week.StartDate,
			week.EndDate,
			week.Title,
			week.VerseRef,
			week.ReciteText,
			week.BookEnabled,
			week.VideoEnabled,
			week.VerseEnabled,
			week.OutlineEnabled,
		})
		weekRow++
		for index, reading := range week.Readings {
			_ = file.SetSheetRow("Readings", fmt.Sprintf("A%d", readingRow), &[]any{
				week.StartDate,
				week.EndDate,
				index + 1,
				reading.Title,
				reading.URL,
				reading.AssetID,
			})
			readingRow++
		}
		for index, video := range week.Videos {
			_ = file.SetSheetRow("Videos", fmt.Sprintf("A%d", videoRow), &[]any{
				week.StartDate,
				week.EndDate,
				index + 1,
				video.Title,
				video.URL,
				video.AssetID,
			})
			videoRow++
		}
		if strings.TrimSpace(week.Outline.Title) != "" || strings.TrimSpace(week.Outline.URL) != "" || week.Outline.AssetID > 0 {
			_ = file.SetSheetRow("Outlines", fmt.Sprintf("A%d", outlineRow), &[]any{
				week.StartDate,
				week.EndDate,
				week.Outline.Title,
				week.Outline.URL,
				week.Outline.Type,
				week.Outline.AssetID,
			})
			outlineRow++
		}
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		writeError(w, http.StatusInternalServerError, "export_study_weeks_failed")
		return
	}
	writeAttachmentHeaders(w, fmt.Sprintf("%s-study-weeks.xlsx", groupExportPrefix(groupID)), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	_, _ = w.Write(buf.Bytes())
}

func (a *app) handleAdminImportStudyWeeksExcel(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxStudyWeeksImportBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "study_weeks_import_too_large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_import_form")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file_required")
		return
	}
	defer file.Close()
	xlsx, err := excelize.OpenReader(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_excel_file")
		return
	}
	defer func() { _ = xlsx.Close() }()
	weeksMap := map[string]*studyWeekInput{}
	var order []string
	if rows, err := xlsx.GetRows("Weeks"); err == nil {
		for _, row := range rows[1:] {
			startDate := excelCell(row, 0)
			endDate := excelCell(row, 1)
			if startDate == "" || endDate == "" {
				continue
			}
			key := weekKey(startDate, endDate)
			weeksMap[key] = &studyWeekInput{
				StartDate:      startDate,
				EndDate:        endDate,
				Title:          excelCell(row, 2),
				VerseRef:       excelCell(row, 3),
				ReciteText:     excelCell(row, 4),
				BookEnabled:    parseFlexibleBool(excelCell(row, 5), true),
				VideoEnabled:   parseFlexibleBool(excelCell(row, 6), true),
				VerseEnabled:   parseFlexibleBool(excelCell(row, 7), true),
				OutlineEnabled: parseFlexibleBool(excelCell(row, 8), true),
				Readings:       []weekTaskBinding{},
				Videos:         []weekTaskBinding{},
				Outline:        weekTaskBinding{Type: "image"},
			}
			order = append(order, key)
		}
	}
	if len(order) == 0 {
		writeError(w, http.StatusBadRequest, "weeks_sheet_required")
		return
	}
	if rows, err := xlsx.GetRows("Readings"); err == nil {
		for _, row := range rows[1:] {
			key := weekKey(excelCell(row, 0), excelCell(row, 1))
			week := weeksMap[key]
			if week == nil {
				continue
			}
			if excelCell(row, 3) == "" && excelCell(row, 4) == "" && excelCell(row, 5) == "" {
				continue
			}
			assetID, _ := strconv.ParseUint(excelCell(row, 5), 10, 64)
			week.Readings = append(week.Readings, weekTaskBinding{
				Title:   excelCell(row, 3),
				URL:     excelCell(row, 4),
				Type:    "pdf",
				AssetID: assetID,
			})
		}
	}
	if rows, err := xlsx.GetRows("Videos"); err == nil {
		for _, row := range rows[1:] {
			key := weekKey(excelCell(row, 0), excelCell(row, 1))
			week := weeksMap[key]
			if week == nil {
				continue
			}
			if excelCell(row, 3) == "" && excelCell(row, 4) == "" && excelCell(row, 5) == "" {
				continue
			}
			assetID, _ := strconv.ParseUint(excelCell(row, 5), 10, 64)
			week.Videos = append(week.Videos, weekTaskBinding{
				Title:   excelCell(row, 3),
				URL:     excelCell(row, 4),
				Type:    "video",
				AssetID: assetID,
			})
		}
	}
	if rows, err := xlsx.GetRows("Outlines"); err == nil {
		for _, row := range rows[1:] {
			key := weekKey(excelCell(row, 0), excelCell(row, 1))
			week := weeksMap[key]
			if week == nil {
				continue
			}
			assetID, _ := strconv.ParseUint(excelCell(row, 5), 10, 64)
			week.Outline = weekTaskBinding{
				Title:   excelCell(row, 2),
				URL:     excelCell(row, 3),
				Type:    firstNonEmpty(excelCell(row, 4), "image"),
				AssetID: assetID,
			}
		}
	}
	weeks := make([]studyWeekInput, 0, len(order))
	for _, key := range order {
		if week := weeksMap[key]; week != nil {
			weeks = append(weeks, *week)
		}
	}
	if err := validateStudyWeekImport(weeks); err != nil {
		status := http.StatusBadRequest
		if err.Error() == "week_overlap" {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx_failed")
		return
	}
	defer tx.Rollback()
	if err := deleteAllStudyWeeksTx(tx, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "study_weeks_import_failed")
		return
	}
	nowTime := time.Now().In(a.location)
	for index := range weeks {
		week := weeks[index]
		res, err := tx.Exec(`INSERT INTO study_weeks (group_id,start_date,end_date,title,verse_ref,recite_text,book_enabled,video_enabled,verse_enabled,outline_enabled,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			groupID, week.StartDate, week.EndDate, week.Title, week.VerseRef, week.ReciteText, week.BookEnabled, week.VideoEnabled, week.VerseEnabled, week.OutlineEnabled, nowTime, nowTime)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "study_weeks_import_failed")
			return
		}
		id64, _ := res.LastInsertId()
		if err := replaceStudyWeekTasksTx(tx, groupID, uint64(id64), week, nowTime); err != nil {
			writeError(w, http.StatusInternalServerError, "study_weeks_import_failed")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "study_weeks_import_failed")
		return
	}
	a.audit(groupID, u.ID, "import_study_weeks_excel", "study_weeks", 0, nil, map[string]any{"weeks": len(weeks)}, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "weeks": len(weeks)})
}

func (a *app) handleAdminExportFeedbacksCSV(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	rows, err := a.db.Query(`SELECT f.created_at,COALESCE(u.username,''),f.name,f.contact,f.message,f.page,f.user_agent
		FROM feedbacks f
		LEFT JOIN users u ON u.id=f.user_id
		LEFT JOIN group_members gm ON gm.user_id=f.user_id AND gm.group_id=? AND gm.status=1
		WHERE f.group_id=? OR (f.group_id IS NULL AND gm.id IS NOT NULL)
		ORDER BY f.id DESC`, groupID, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_feedbacks_failed")
		return
	}
	defer rows.Close()
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{"提交时间", "账号", "姓名", "联系方式", "页面", "反馈内容", "User-Agent"})
	for rows.Next() {
		var created time.Time
		var username, name, contact, message, page, userAgent string
		if err := rows.Scan(&created, &username, &name, &contact, &message, &page, &userAgent); err != nil {
			writeError(w, http.StatusInternalServerError, "export_feedbacks_failed")
			return
		}
		_ = writer.Write([]string{
			created.In(a.location).Format("2006-01-02 15:04:05"),
			username,
			name,
			contact,
			page,
			message,
			userAgent,
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		writeError(w, http.StatusInternalServerError, "export_feedbacks_failed")
		return
	}
	writeAttachmentHeaders(w, fmt.Sprintf("%s-feedbacks.csv", groupExportPrefix(groupID)), "text/csv; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (a *app) exportBackupMembers(groupID uint64) ([]localBackupMember, error) {
	rows, err := a.db.Query(`SELECT u.username,u.display_name,u.name_pinyin
		FROM group_members m JOIN users u ON u.id=m.user_id
		WHERE m.group_id=? AND m.status=1
		ORDER BY m.member_name,u.username`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roleMap := map[string][]string{}
	roleRows, err := a.db.Query(`SELECT u.username,r.role
		FROM user_group_roles r JOIN users u ON u.id=r.user_id
		WHERE r.group_id=? AND r.role IN (?,?)
		ORDER BY u.username,r.role`, groupID, roleGroupAdmin, roleGroupLeader)
	if err == nil {
		defer roleRows.Close()
		for roleRows.Next() {
			var username, role string
			_ = roleRows.Scan(&username, &role)
			roleMap[username] = append(roleMap[username], role)
		}
	}
	var items []localBackupMember
	for rows.Next() {
		var username, displayName, namePinyin string
		if err := rows.Scan(&username, &displayName, &namePinyin); err != nil {
			return nil, err
		}
		items = append(items, localBackupMember{
			Username:    username,
			DisplayName: displayName,
			NamePinyin:  namePinyin,
			Roles:       roleMap[username],
		})
	}
	return items, rows.Err()
}

func (a *app) exportBackupCheckins(groupID uint64) ([]localBackupCheckin, error) {
	rows, err := a.db.Query(`SELECT u.username,c.logical_date,c.checkin_time,c.task_type,c.part,c.detail,COALESCE(c.note,''),c.is_retro
		FROM checkin_records c JOIN users u ON u.id=c.user_id
		WHERE c.group_id=? AND c.deleted_at IS NULL
		ORDER BY c.logical_date,c.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []localBackupCheckin
	for rows.Next() {
		var username, logicalDate, taskType, part, detail, note string
		var checkinTime time.Time
		var isRetro bool
		if err := rows.Scan(&username, &logicalDate, &checkinTime, &taskType, &part, &detail, &note, &isRetro); err != nil {
			return nil, err
		}
		items = append(items, localBackupCheckin{
			Username:    username,
			LogicalDate: logicalDate,
			CheckinTime: checkinTime.Format(time.RFC3339),
			TaskType:    taskType,
			Part:        part,
			Detail:      detail,
			Note:        note,
			IsRetro:     isRetro,
		})
	}
	return items, rows.Err()
}

func (a *app) exportBackupFeedbacks(groupID uint64) ([]localBackupFeedback, error) {
	rows, err := a.db.Query(`SELECT COALESCE(u.username,''),f.name,f.contact,f.message,f.page,f.user_agent,f.created_at
		FROM feedbacks f
		LEFT JOIN users u ON u.id=f.user_id
		LEFT JOIN group_members gm ON gm.user_id=f.user_id AND gm.group_id=? AND gm.status=1
		WHERE f.group_id=? OR (f.group_id IS NULL AND gm.id IS NOT NULL)
		ORDER BY f.id`, groupID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []localBackupFeedback
	for rows.Next() {
		var username, name, contact, message, page, userAgent string
		var created time.Time
		if err := rows.Scan(&username, &name, &contact, &message, &page, &userAgent, &created); err != nil {
			return nil, err
		}
		items = append(items, localBackupFeedback{
			Username:  username,
			Name:      name,
			Contact:   contact,
			Message:   message,
			Page:      page,
			UserAgent: userAgent,
			CreatedAt: created.Format(time.RFC3339),
		})
	}
	return items, rows.Err()
}

func (a *app) exportBackupAssets(groupID uint64) ([]localBackupAsset, error) {
	rows, err := a.db.Query(`SELECT category,title,original_name,storage_path,mime_type,file_size FROM assets WHERE group_id=? ORDER BY category,title,id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []localBackupAsset
	for rows.Next() {
		var item localBackupAsset
		if err := rows.Scan(&item.Category, &item.Title, &item.OriginalName, &item.StoragePath, &item.MimeType, &item.FileSize); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *app) handleAdminExportLocalBackupJSON(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var code, name, description string
	if err := a.db.QueryRow(`SELECT code,name,description FROM study_groups WHERE id=?`, groupID).Scan(&code, &name, &description); err != nil {
		writeError(w, http.StatusInternalServerError, "export_backup_failed")
		return
	}
	settings, err := a.groupLearningConfig(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_backup_failed")
		return
	}
	members, err := a.exportBackupMembers(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_backup_failed")
		return
	}
	weeks, err := a.listStudyWeekInputs(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_backup_failed")
		return
	}
	checkins, err := a.exportBackupCheckins(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_backup_failed")
		return
	}
	feedbacks, err := a.exportBackupFeedbacks(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_backup_failed")
		return
	}
	assets, err := a.exportBackupAssets(groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_backup_failed")
		return
	}
	payload := localBackupPayload{
		Version:    1,
		ExportedAt: time.Now().In(a.location).Format(time.RFC3339),
		Group: map[string]any{
			"id":          groupID,
			"code":        code,
			"name":        name,
			"description": description,
		},
		Settings:  settings,
		Members:   members,
		Weeks:     weeks,
		Checkins:  checkins,
		Feedbacks: feedbacks,
		Assets:    assets,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_backup_failed")
		return
	}
	writeAttachmentHeaders(w, fmt.Sprintf("%s-local-backup.json", code), "application/json; charset=utf-8")
	_, _ = w.Write(body)
}

func (a *app) ensureGroupMemberUserTx(tx *sql.Tx, groupID uint64, member localBackupMember, actorID uint64) (uint64, error) {
	username := normalizeUsername(member.Username)
	if username == "" {
		return 0, errors.New("username_required")
	}
	displayName := firstNonEmpty(strings.TrimSpace(member.DisplayName), username)
	namePinyin := firstNonEmpty(strings.TrimSpace(member.NamePinyin), username)
	var userID uint64
	err := tx.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		hash, err := a.groupDefaultPasswordHashTx(tx, groupID)
		if err != nil {
			return 0, err
		}
		if strings.TrimSpace(hash) == "" {
			return 0, errors.New("group_default_password_missing")
		}
		now := nowSQL()
		res, err := tx.Exec(`INSERT INTO users (username,display_name,name_pinyin,password_hash,must_change_password,created_by,created_at,updated_at)
			VALUES (?,?,?,?,1,?,?,?)`, username, displayName, namePinyin, hash, actorID, now, now)
		if err != nil {
			return 0, err
		}
		id64, _ := res.LastInsertId()
		userID = uint64(id64)
	} else if err != nil {
		return 0, err
	}
	if err := addMemberTx(tx, groupID, userID, displayName, actorID); err != nil {
		return 0, err
	}
	return userID, nil
}

func (a *app) handleAdminImportLocalBackupJSON(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID := requireGroupID(w, u)
	if groupID == 0 {
		return
	}
	var payload localBackupPayload
	if !readJSONLimit(w, r, &payload, maxBackupBytes) {
		return
	}
	if err := validateLocalBackupPayload(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tx_failed")
		return
	}
	defer tx.Rollback()
	if payload.Settings != nil {
		if err := upsertGroupLearningConfigTx(tx, groupID, payload.Settings); err != nil {
			writeError(w, http.StatusInternalServerError, "backup_import_failed")
			return
		}
	}
	roleAssignments := map[uint64][]string{}
	for _, member := range payload.Members {
		userID, err := a.ensureGroupMemberUserTx(tx, groupID, member, u.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "backup_import_failed")
			return
		}
		roleAssignments[userID] = append([]string{}, member.Roles...)
	}
	// Group leaders are roster-controlled and must never be removed or granted by a group backup.
	if _, err := tx.Exec(`DELETE FROM user_group_roles WHERE group_id=? AND role=?`, groupID, roleGroupAdmin); err != nil {
		writeError(w, http.StatusInternalServerError, "backup_import_failed")
		return
	}
	for userID, roles := range roleAssignments {
		for _, role := range roles {
			role = strings.TrimSpace(role)
			if role != roleGroupAdmin {
				continue
			}
			if _, err := tx.Exec(`INSERT IGNORE INTO user_group_roles (group_id,user_id,role,created_at) VALUES (?,?,?,?)`, groupID, userID, role, nowSQL()); err != nil {
				writeError(w, http.StatusInternalServerError, "backup_import_failed")
				return
			}
		}
	}
	if err := deleteAllStudyWeeksTx(tx, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "backup_import_failed")
		return
	}
	nowTime := time.Now().In(a.location)
	for _, week := range payload.Weeks {
		res, err := tx.Exec(`INSERT INTO study_weeks (group_id,start_date,end_date,title,verse_ref,recite_text,book_enabled,video_enabled,verse_enabled,outline_enabled,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			groupID, week.StartDate, week.EndDate, week.Title, week.VerseRef, week.ReciteText, week.BookEnabled, week.VideoEnabled, week.VerseEnabled, week.OutlineEnabled, nowTime, nowTime)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "backup_import_failed")
			return
		}
		id64, _ := res.LastInsertId()
		if err := replaceStudyWeekTasksTx(tx, groupID, uint64(id64), week, nowTime); err != nil {
			writeError(w, http.StatusInternalServerError, "backup_import_failed")
			return
		}
	}
	if _, err := tx.Exec(`UPDATE checkin_records SET deleted_at=?, active_key=id, updated_at=? WHERE group_id=? AND deleted_at IS NULL`, nowSQL(), nowSQL(), groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "backup_import_failed")
		return
	}
	userIDs := map[string]uint64{}
	for userID, roles := range roleAssignments {
		_ = roles
		var username string
		if err := tx.QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&username); err == nil {
			userIDs[username] = userID
		}
	}
	importedCheckins := 0
	skippedCheckins := 0
	for _, checkin := range payload.Checkins {
		userID := userIDs[normalizeUsername(checkin.Username)]
		if userID == 0 {
			skippedCheckins++
			continue
		}
		checkinTime := parseTimeOrNow(checkin.CheckinTime, nowTime)
		taskType := strings.ToLower(strings.TrimSpace(checkin.TaskType))
		result, err := tx.Exec(`INSERT IGNORE INTO checkin_records (group_id,user_id,task_id,week_id,logical_date,checkin_time,task_type,status,is_retro,detail,note,part,source,created_by,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			groupID, userID, nil, nil, checkin.LogicalDate, checkinTime, taskType, "done", checkin.IsRetro, truncate(checkin.Detail, 1000), truncate(checkin.Note, 4000), truncate(checkin.Part, 64), "import", u.ID, checkinTime, checkinTime)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "backup_import_failed")
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			skippedCheckins++
		} else {
			importedCheckins++
		}
	}
	if _, err := tx.Exec(`DELETE FROM feedbacks WHERE group_id=?`, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "backup_import_failed")
		return
	}
	for _, feedback := range payload.Feedbacks {
		var userID any = nil
		if id := userIDs[normalizeUsername(feedback.Username)]; id > 0 {
			userID = id
		}
		createdAt := parseTimeOrNow(feedback.CreatedAt, nowTime)
		if _, err := tx.Exec(`INSERT INTO feedbacks (group_id,user_id,name,contact,message,page,user_agent,created_at)
			VALUES (?,?,?,?,?,?,?,?)`, groupID, userID, feedback.Name, feedback.Contact, feedback.Message, feedback.Page, feedback.UserAgent, createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "backup_import_failed")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "backup_import_failed")
		return
	}
	a.audit(groupID, u.ID, "import_local_backup", "study_groups", groupID, nil, map[string]any{
		"members":   len(payload.Members),
		"weeks":     len(payload.Weeks),
		"checkins":  importedCheckins,
		"skipped":   skippedCheckins,
		"feedbacks": len(payload.Feedbacks),
	}, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported_checkins": importedCheckins, "skipped_checkins": skippedCheckins, "assets_metadata_skipped": len(payload.Assets)})
}

func (a *app) handleSuperListGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query("SELECT id, code, name FROM study_groups ORDER BY id")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "groups_failed")
		return
	}
	defer rows.Close()
	var groups []group
	for rows.Next() {
		var g group
		_ = rows.Scan(&g.ID, &g.Code, &g.Name)
		groups = append(groups, g)
	}
	writeJSON(w, http.StatusOK, map[string]any{"study_groups": groups})
}

func (a *app) handleSuperCreateGroup(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	password := randomPassword(8)
	hash, err := hashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_failed")
		return
	}
	now := nowSQL()
	res, err := a.db.Exec("INSERT INTO study_groups (code,name,description,default_password_hash,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?)", req.Code, req.Name, req.Description, hash, u.ID, now, now)
	if err != nil {
		writeError(w, http.StatusConflict, "group_create_failed")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "default_password": password})
}

func (a *app) handleSuperSetGroupDefaultPassword(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	affected, err := a.setGroupDefaultPassword(groupID, req.Password, false, u.ID, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "affected_users": affected})
}

func (a *app) handleSuperListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query("SELECT id, username, display_name, is_super_admin, status FROM users ORDER BY id LIMIT 500")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "users_failed")
		return
	}
	defer rows.Close()
	var users []map[string]any
	for rows.Next() {
		var id uint64
		var username, display string
		var super bool
		var status int
		_ = rows.Scan(&id, &username, &display, &super, &status)
		users = append(users, map[string]any{"id": id, "username": username, "display_name": display, "is_super_admin": super, "status": status})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (a *app) handleSuperCreateUser(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	var req struct {
		Username     string `json:"username"`
		DisplayName  string `json:"display_name"`
		NamePinyin   string `json:"name_pinyin"`
		Password     string `json:"password"`
		GroupID      uint64 `json:"group_id"`
		Role         string `json:"role"`
		IsSuperAdmin bool   `json:"is_super_admin"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	password := req.Password
	if password == "" && req.GroupID > 0 {
		hash, err := a.groupDefaultPasswordHash(req.GroupID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "password_required")
			return
		}
		returnedID, err := a.createUserWithHash(req.Username, req.DisplayName, req.NamePinyin, hash, req.IsSuperAdmin, u.ID)
		if err != nil {
			writeError(w, http.StatusConflict, "user_create_failed")
			return
		}
		if req.GroupID > 0 {
			_ = a.addMember(req.GroupID, returnedID, req.DisplayName, u.ID)
			if req.Role != "" {
				_, _ = a.db.Exec("INSERT IGNORE INTO user_group_roles (group_id,user_id,role,created_at) VALUES (?,?,?,?)", req.GroupID, returnedID, req.Role, nowSQL())
			}
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": returnedID})
		return
	}
	if password == "" {
		password = randomPassword(8)
	}
	hash, err := hashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password_failed")
		return
	}
	id, err := a.createUserWithHash(req.Username, req.DisplayName, req.NamePinyin, hash, req.IsSuperAdmin, u.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "user_create_failed")
		return
	}
	if req.GroupID > 0 {
		_ = a.addMember(req.GroupID, id, req.DisplayName, u.ID)
		if req.Role != "" {
			_, _ = a.db.Exec("INSERT IGNORE INTO user_group_roles (group_id,user_id,role,created_at) VALUES (?,?,?,?)", req.GroupID, id, req.Role, nowSQL())
		}
	}
	resp := map[string]any{"id": id}
	if req.Password == "" {
		resp["initial_password"] = password
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (a *app) handleSuperResetAllPasswords(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	var req struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Confirm != "RESET_ALL_NON_SUPER_ADMINS" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "confirmation_required")
		return
	}
	hash, _ := hashPassword(req.Password)
	res, err := a.db.Exec("UPDATE users SET password_hash=?, must_change_password=1, auth_version=auth_version+1, updated_at=? WHERE is_super_admin=0", hash, nowSQL())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reset_failed")
		return
	}
	affected, _ := res.RowsAffected()
	a.audit(0, u.ID, "reset_all_passwords", "users", 0, nil, map[string]any{"affected": affected}, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "affected_users": affected})
}

func (a *app) handleSuperAddGroupMember(w http.ResponseWriter, r *http.Request) {
	u := mustUser(r)
	groupID, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var req struct {
		UserID     uint64 `json:"user_id"`
		MemberName string `json:"member_name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.addMember(groupID, req.UserID, req.MemberName, u.ID); err != nil {
		writeError(w, http.StatusConflict, "member_add_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (a *app) handleSuperSetLeader(w http.ResponseWriter, r *http.Request) {
	a.superSetLeaderRole(w, r, true)
}

func (a *app) handleSuperUnsetLeader(w http.ResponseWriter, r *http.Request) {
	a.superSetLeaderRole(w, r, false)
}

func (a *app) superSetLeaderRole(w http.ResponseWriter, r *http.Request, grant bool) {
	groupID, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var userID uint64
	if grant {
		var req struct {
			UserID uint64 `json:"user_id"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		userID = req.UserID
	} else {
		userID, _ = strconv.ParseUint(r.PathValue("user_id"), 10, 64)
	}
	if grant {
		_, _ = a.db.Exec("INSERT IGNORE INTO user_group_roles (group_id,user_id,role,created_at) VALUES (?,?,?,?)", groupID, userID, roleGroupLeader, nowSQL())
	} else {
		_, _ = a.db.Exec("DELETE FROM user_group_roles WHERE group_id=? AND user_id=? AND role=?", groupID, userID, roleGroupLeader)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		claims, err := a.verifyToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		u, err := a.loadCurrentUser(claims.UserID, claims.CurrentGroupID)
		if err != nil || claims.AuthVersion == 0 || claims.AuthVersion != u.AuthVersion {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if u.MustChangePassword && r.URL.Path != "/api/auth/me" && r.URL.Path != "/api/auth/change-password" {
			writeError(w, http.StatusForbidden, "password_change_required")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), currentUserKey, u)))
	}
}

func (a *app) requireSuper(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !mustUser(r).IsSuperAdmin {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	}
}

func (a *app) requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := mustUser(r)
		if u.IsSuperAdmin || hasRole(u.Roles, role) || (role == roleGroupAdmin && hasRole(u.Roles, roleGroupLeader)) {
			next(w, r)
			return
		}
		writeError(w, http.StatusForbidden, "forbidden")
	}
}

func mustUser(r *http.Request) currentUser {
	return r.Context().Value(currentUserKey).(currentUser)
}

func (a *app) loadCurrentUser(userID, currentGroupID uint64) (currentUser, error) {
	var u currentUser
	var status int
	var defaultGroupID sql.NullInt64
	var avatarPath string
	err := a.db.QueryRow("SELECT id, username, display_name, is_super_admin, default_group_id, must_change_password, auth_version, status, email, avatar_path FROM users WHERE id=?", userID).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsSuperAdmin, &defaultGroupID, &u.MustChangePassword, &u.AuthVersion, &status, &u.Email, &avatarPath)
	if err != nil || status != 1 {
		return u, errors.New("user_not_found")
	}
	if defaultGroupID.Valid && defaultGroupID.Int64 > 0 {
		u.DefaultGroupID = uint64(defaultGroupID.Int64)
	}
	if avatarPath != "" {
		u.AvatarURL = "/api/avatars/" + filepath.Base(avatarPath)
	}
	u.Groups, _ = a.visibleGroups(userID, u.IsSuperAdmin)
	if currentGroupID > 0 && containsGroup(u.Groups, currentGroupID) {
		u.CurrentGroupID = currentGroupID
	}
	if u.CurrentGroupID > 0 {
		u.Roles, _ = a.userRoles(userID, u.CurrentGroupID)
	}
	return u, nil
}

func (a *app) visibleGroups(userID uint64, isSuperAdmin bool) ([]group, error) {
	if isSuperAdmin {
		return a.allGroups()
	}
	return a.userGroups(userID)
}

func (a *app) allGroups() ([]group, error) {
	rows, err := a.db.Query("SELECT id,code,name FROM study_groups WHERE status=1 ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []group
	for rows.Next() {
		var g group
		_ = rows.Scan(&g.ID, &g.Code, &g.Name)
		out = append(out, g)
	}
	return out, nil
}

func (a *app) userGroups(userID uint64) ([]group, error) {
	rows, err := a.db.Query("SELECT g.id,g.code,g.name FROM study_groups g JOIN group_members m ON m.group_id=g.id WHERE m.user_id=? AND m.status=1 AND g.status=1 ORDER BY g.id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []group
	for rows.Next() {
		var g group
		_ = rows.Scan(&g.ID, &g.Code, &g.Name)
		out = append(out, g)
	}
	return out, nil
}

func (a *app) userRoles(userID, groupID uint64) ([]string, error) {
	rows, err := a.db.Query("SELECT role FROM user_group_roles WHERE user_id=? AND group_id=?", userID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		_ = rows.Scan(&role)
		roles = append(roles, role)
	}
	roles = append(roles, roleMember)
	return roles, nil
}

func requireGroupID(w http.ResponseWriter, u currentUser) uint64 {
	if u.CurrentGroupID == 0 {
		writeError(w, http.StatusBadRequest, "group_required")
		return 0
	}
	return u.CurrentGroupID
}

func (a *app) listMembers(groupID uint64) ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT m.id,u.id,u.username,u.display_name,m.member_name,u.is_super_admin,u.avatar_path
		FROM group_members m JOIN users u ON u.id=m.user_id
		WHERE m.group_id=? AND m.status=1 ORDER BY m.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []map[string]any
	for rows.Next() {
		var mid, uid uint64
		var username, display, memberName, avatarPath string
		var super bool
		_ = rows.Scan(&mid, &uid, &username, &display, &memberName, &super, &avatarPath)
		roles, _ := a.userRoles(uid, groupID)
		avatarURL := ""
		if avatarPath != "" {
			avatarURL = "/api/avatars/" + filepath.Base(avatarPath)
		}
		members = append(members, map[string]any{"member_id": mid, "user_id": uid, "username": username, "display_name": display, "member_name": firstNonEmpty(memberName, display), "avatar_url": avatarURL, "is_super_admin": super, "roles": roles})
	}
	return members, nil
}

func (a *app) currentWeek(groupID uint64) (map[string]any, error) {
	today := time.Now().In(a.location).Format("2006-01-02")
	return a.currentWeekAt(groupID, today)
}

func splitWeekTaskBindings(tasks []map[string]any) ([]weekTaskBinding, []weekTaskBinding, weekTaskBinding) {
	var readings []weekTaskBinding
	var videos []weekTaskBinding
	var outline weekTaskBinding
	for _, task := range tasks {
		binding := taskBindingFromMap(task)
		switch asString(task["task_type"]) {
		case "weekly_book":
			if binding.Title != "" || binding.URL != "" || binding.AssetID > 0 {
				readings = append(readings, binding)
			}
		case "weekly_video":
			if binding.Title != "" || binding.URL != "" || binding.AssetID > 0 {
				videos = append(videos, binding)
			}
		case "weekly_outline":
			outline = binding
		}
	}
	return readings, videos, outline
}

func taskBindingFromMap(task map[string]any) weekTaskBinding {
	binding := weekTaskBinding{
		Title: asString(task["title"]),
		URL:   asString(task["content"]),
		Type:  inferTaskBindingType(asString(task["task_type"]), asString(task["content"]), ""),
	}
	if asset := firstTaskAsset(task["assets"]); asset != nil {
		if id, ok := asset["id"].(uint64); ok {
			binding.AssetID = id
		}
		if binding.Title == "" {
			binding.Title = firstNonEmpty(asString(asset["title"]), asString(asset["original_name"]))
		}
		binding.Type = inferTaskBindingType(asString(task["task_type"]), binding.URL, firstNonEmpty(asString(asset["original_name"]), asString(asset["title"])))
	}
	return binding
}

func firstTaskAsset(raw any) map[string]any {
	switch items := raw.(type) {
	case []map[string]any:
		if len(items) > 0 {
			return items[0]
		}
	case []any:
		if len(items) > 0 {
			if asset, ok := items[0].(map[string]any); ok {
				return asset
			}
		}
	}
	return nil
}

func inferTaskBindingType(taskType, urlValue, fileName string) string {
	switch taskType {
	case "weekly_video":
		return "video"
	case "weekly_outline":
		return "image"
	}
	value := strings.ToLower(firstNonEmpty(fileName, urlValue))
	switch {
	case strings.HasSuffix(value, ".md"):
		return "markdown"
	case strings.HasSuffix(value, ".mp4"), strings.HasSuffix(value, ".webm"), strings.HasSuffix(value, ".mov"), strings.HasSuffix(value, ".m4v"):
		return "video"
	case strings.HasSuffix(value, ".png"), strings.HasSuffix(value, ".jpg"), strings.HasSuffix(value, ".jpeg"), strings.HasSuffix(value, ".webp"):
		return "image"
	default:
		return "pdf"
	}
}

func (a *app) currentWeekAt(groupID uint64, date string) (map[string]any, error) {
	var id uint64
	var start, end time.Time
	var title, verse string
	var recite sql.NullString
	err := a.db.QueryRow(`SELECT id,start_date,end_date,title,verse_ref,recite_text FROM study_weeks WHERE group_id=? AND start_date <= ? AND end_date >= ? ORDER BY start_date DESC LIMIT 1`, groupID, date, date).
		Scan(&id, &start, &end, &title, &verse, &recite)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "start": start.Format("2006-01-02"), "end": end.Format("2006-01-02"), "title": title, "verse_ref": verse, "recite_text": recite.String}, nil
}

func (a *app) weekTasks(groupID, weekID uint64) ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT id,task_type,title,COALESCE(content,''),enabled FROM study_tasks WHERE group_id=? AND week_id=? AND enabled=1 ORDER BY sort_order,id`, groupID, weekID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []map[string]any
	for rows.Next() {
		var id uint64
		var taskType, title, content string
		var enabled bool
		_ = rows.Scan(&id, &taskType, &title, &content, &enabled)
		assets, _ := a.taskAssets(groupID, id)
		tasks = append(tasks, map[string]any{"id": id, "task_type": taskType, "title": title, "content": content, "enabled": enabled, "assets": assets})
	}
	return tasks, rows.Err()
}

func (a *app) taskAssets(groupID, taskID uint64) ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT a.id,a.category,a.title,a.original_name,ta.usage_type
		FROM task_assets ta JOIN assets a ON a.id=ta.asset_id
		WHERE ta.group_id=? AND ta.task_id=?
		ORDER BY ta.sort_order,ta.id`, groupID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []map[string]any
	for rows.Next() {
		var id uint64
		var category, title, original, usage string
		_ = rows.Scan(&id, &category, &title, &original, &usage)
		assets = append(assets, map[string]any{"id": id, "category": category, "title": title, "original_name": original, "usage_type": usage})
	}
	return assets, rows.Err()
}

func deleteStudyWeekTasksTx(tx *sql.Tx, groupID, weekID uint64) error {
	if _, err := tx.Exec(`UPDATE checkin_records c
		JOIN study_tasks st ON st.id=c.task_id AND st.group_id=c.group_id
		SET c.task_id=NULL
		WHERE st.group_id=? AND st.week_id=?`, groupID, weekID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE ta FROM task_assets ta
		JOIN study_tasks st ON st.id=ta.task_id
		WHERE st.group_id=? AND st.week_id=?`, groupID, weekID); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM study_tasks WHERE group_id=? AND week_id=?`, groupID, weekID)
	return err
}

func replaceStudyWeekTasksTx(tx *sql.Tx, groupID, weekID uint64, req studyWeekInput, now time.Time) error {
	if err := deleteStudyWeekTasksTx(tx, groupID, weekID); err != nil {
		return err
	}
	if req.BookEnabled {
		order := 1
		for _, reading := range req.Readings {
			if strings.TrimSpace(reading.Title) == "" && reading.AssetID == 0 && strings.TrimSpace(reading.URL) == "" {
				continue
			}
			taskID, err := insertStudyTaskTx(tx, groupID, weekID, "weekly_book", firstNonEmpty(strings.TrimSpace(reading.Title), "周读物"), strings.TrimSpace(reading.URL), order, now)
			if err != nil {
				return err
			}
			if reading.AssetID > 0 {
				if err := linkTaskAssetTx(tx, groupID, taskID, reading.AssetID, "reading", order, now); err != nil {
					return err
				}
			}
			order++
		}
	}
	if req.VideoEnabled {
		order := 1
		for _, video := range req.Videos {
			if strings.TrimSpace(video.Title) == "" && video.AssetID == 0 && strings.TrimSpace(video.URL) == "" {
				continue
			}
			taskID, err := insertStudyTaskTx(tx, groupID, weekID, "weekly_video", firstNonEmpty(strings.TrimSpace(video.Title), "本周视频"), strings.TrimSpace(video.URL), order, now)
			if err != nil {
				return err
			}
			if video.AssetID > 0 {
				if err := linkTaskAssetTx(tx, groupID, taskID, video.AssetID, "video", order, now); err != nil {
					return err
				}
			}
			order++
		}
	}
	if req.VerseEnabled && strings.TrimSpace(req.VerseRef) != "" {
		if _, err := insertStudyTaskTx(tx, groupID, weekID, "weekly_verse", strings.TrimSpace(req.VerseRef), strings.TrimSpace(req.ReciteText), 1, now); err != nil {
			return err
		}
	}
	if req.OutlineEnabled && (strings.TrimSpace(req.Outline.Title) != "" || req.Outline.AssetID > 0 || strings.TrimSpace(req.Outline.URL) != "") {
		taskID, err := insertStudyTaskTx(tx, groupID, weekID, "weekly_outline", firstNonEmpty(strings.TrimSpace(req.Outline.Title), "提纲背诵"), strings.TrimSpace(req.Outline.URL), 1, now)
		if err != nil {
			return err
		}
		if req.Outline.AssetID > 0 {
			if err := linkTaskAssetTx(tx, groupID, taskID, req.Outline.AssetID, "outline", 1, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertStudyTaskTx(tx *sql.Tx, groupID, weekID uint64, taskType, title, content string, sortOrder int, now time.Time) (uint64, error) {
	res, err := tx.Exec(`INSERT INTO study_tasks (group_id,week_id,task_type,title,content,required,enabled,sort_order,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, groupID, weekID, taskType, title, content, true, true, sortOrder, now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func linkTaskAssetTx(tx *sql.Tx, groupID, taskID, assetID uint64, usageType string, sortOrder int, now time.Time) error {
	if assetID == 0 {
		return nil
	}
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(1) FROM assets WHERE id=? AND group_id=?`, assetID, groupID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return errors.New("asset_not_found")
	}
	_, err := tx.Exec(`INSERT INTO task_assets (group_id,task_id,asset_id,usage_type,sort_order,created_at)
		VALUES (?,?,?,?,?,?)`, groupID, taskID, assetID, usageType, sortOrder, now)
	return err
}

func sanitizeUploadName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case strings.ContainsRune("._-()[]（）【】 ", r):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.TrimSpace(b.String())
}

func normalizeAssetCategory(value string) (string, error) {
	category := strings.ToLower(strings.TrimSpace(value))
	if category == "" {
		category = "uploaded"
	}
	for _, allowed := range []string{"book", "markdown", "handout", "outline", "video", "mentor", "uploaded"} {
		if category == allowed {
			return category, nil
		}
	}
	return "", errors.New("invalid_asset_category")
}

func allowedUploadExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".pdf", ".md", ".markdown", ".png", ".jpg", ".jpeg", ".webp", ".gif", ".mp4", ".m4v", ".mov":
		return true
	default:
		return false
	}
}

func allowedUploadContentType(ext, detected string) bool {
	detected = strings.ToLower(strings.TrimSpace(strings.Split(detected, ";")[0]))
	switch strings.ToLower(ext) {
	case ".pdf":
		return detected == "application/pdf"
	case ".md", ".markdown":
		return detected == "text/plain" || detected == "application/octet-stream"
	case ".png":
		return detected == "image/png"
	case ".jpg", ".jpeg":
		return detected == "image/jpeg"
	case ".webp":
		return detected == "image/webp" || detected == "application/octet-stream"
	case ".gif":
		return detected == "image/gif"
	case ".mp4", ".m4v", ".mov":
		return strings.HasPrefix(detected, "video/") || detected == "application/octet-stream"
	default:
		return false
	}
}

func (a *app) groupLearningConfig(groupID uint64) (map[string]any, error) {
	var raw sql.NullString
	err := a.db.QueryRow(`SELECT settings FROM group_settings WHERE group_id=?`, groupID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return map[string]any{}, nil
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(raw.String), &settings); err != nil {
		return map[string]any{}, nil
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, nil
}

func (a *app) upsertGroupLearningConfig(groupID uint64, settings map[string]any) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	now := nowSQL()
	_, err = a.db.Exec(`INSERT INTO group_settings (group_id,settings,created_at,updated_at)
		VALUES (?,?,?,?)
		ON DUPLICATE KEY UPDATE settings=VALUES(settings), updated_at=VALUES(updated_at)`,
		groupID, string(payload), now, now)
	return err
}

func (a *app) scanStaticLibrarySection(key, label, subdir, publicPrefix string, extensions []string) map[string]any {
	root := a.contentRoot
	if subdir != "" {
		root = filepath.Join(root, subdir)
	}
	allowed := map[string]bool{}
	for _, ext := range extensions {
		allowed[strings.ToLower(ext)] = true
	}
	items := []map[string]any{}
	folders := []map[string]any{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." {
			return nil
		}
		if entry.IsDir() {
			if subdir != "" {
				folders = append(folders, map[string]any{"path": filepath.ToSlash(relative), "label": entry.Name()})
			}
			return nil
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if len(allowed) > 0 && !allowed[ext] {
			return nil
		}
		title := strings.TrimSuffix(name, ext)
		if strings.HasPrefix(title, "[B311]") {
			title = strings.TrimPrefix(title, "[B311]")
		}
		urlPath := publicPrefix
		if !strings.HasPrefix(urlPath, "/") {
			urlPath = "/" + strings.TrimPrefix(urlPath, "/")
		}
		if urlPath == "/" {
			urlPath = "/" + encodeURLPath(filepath.ToSlash(relative))
		} else {
			urlPath = strings.TrimRight(urlPath, "/") + "/" + encodeURLPath(filepath.ToSlash(relative))
		}
		folder := filepath.ToSlash(filepath.Dir(relative))
		if folder == "." {
			folder = ""
		}
		items = append(items, map[string]any{
			"title":         title,
			"original_name": name,
			"relative_path": filepath.ToSlash(relative),
			"folder":        folder,
			"root":          subdir,
			"url":           urlPath,
			"category":      key,
			"source":        "static",
			"type":          inferTaskBindingType("", "", name),
		})
		return nil
	})
	if err != nil {
		return map[string]any{"key": key, "label": label, "managed_root": subdir, "items": []map[string]any{}, "folders": []map[string]any{}, "count": 0}
	}
	sort.Slice(items, func(i, j int) bool {
		return asString(items[i]["relative_path"]) < asString(items[j]["relative_path"])
	})
	sort.Slice(folders, func(i, j int) bool { return asString(folders[i]["path"]) < asString(folders[j]["path"]) })
	return map[string]any{"key": key, "label": label, "managed_root": subdir, "items": items, "folders": folders, "count": len(items)}
}

func encodeURLPath(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func (a *app) uploadedLibrarySections(groupID uint64) ([]map[string]any, error) {
	rows, err := a.db.Query(`SELECT id,category,title,original_name,mime_type FROM assets WHERE group_id=? ORDER BY category,title,id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grouped := map[string][]map[string]any{}
	for rows.Next() {
		var id uint64
		var category, title, original, mt string
		if err := rows.Scan(&id, &category, &title, &original, &mt); err != nil {
			return nil, err
		}
		key := firstNonEmpty(category, "uploaded")
		grouped[key] = append(grouped[key], map[string]any{
			"id":            id,
			"title":         title,
			"original_name": original,
			"url":           fmt.Sprintf("/api/assets/%d/download", id),
			"category":      key,
			"source":        "uploaded",
			"type":          inferTaskBindingType("", "", firstNonEmpty(original, mt)),
		})
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var sections []map[string]any
	for _, key := range keys {
		label := "上传资源"
		switch key {
		case "markdown":
			label = "上传 Markdown"
		case "book":
			label = "上传 PDF 读物"
		case "video":
			label = "上传视频"
		case "handout":
			label = "上传讲义"
		case "outline":
			label = "上传提纲图片"
		}
		items := grouped[key]
		sections = append(sections, map[string]any{"key": "uploaded_" + key, "label": label, "items": items, "count": len(items)})
	}
	return sections, nil
}

func (a *app) setGroupDefaultPassword(groupID uint64, password string, includeLeaders bool, actorID uint64, r *http.Request) (int64, error) {
	if len(password) < 8 {
		return 0, errors.New("password_too_short")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return 0, err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE study_groups SET default_password_hash=?, updated_at=? WHERE id=?", hash, nowSQL(), groupID); err != nil {
		return 0, err
	}
	query := `UPDATE users u
		JOIN group_members m ON m.user_id=u.id AND m.group_id=? AND m.status=1
		LEFT JOIN user_group_roles r ON r.user_id=u.id AND r.group_id=? AND r.role=?
		SET u.password_hash=?, u.must_change_password=1, u.auth_version=u.auth_version+1, u.updated_at=?
		WHERE u.is_super_admin=0
		  AND r.id IS NULL
		  AND (SELECT COUNT(*) FROM group_members gm WHERE gm.user_id=u.id AND gm.status=1)=1`
	res, err := tx.Exec(query, groupID, groupID, roleGroupLeader, hash, nowSQL())
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	a.audit(groupID, actorID, "set_group_default_password", "study_groups", groupID, nil, map[string]any{"affected_users": affected}, r)
	return affected, nil
}

func (a *app) groupDefaultPasswordHash(groupID uint64) (string, error) {
	var hash string
	err := a.db.QueryRow("SELECT default_password_hash FROM study_groups WHERE id=?", groupID).Scan(&hash)
	return hash, err
}

func (a *app) groupDefaultPasswordHashTx(tx *sql.Tx, groupID uint64) (string, error) {
	var hash string
	err := tx.QueryRow("SELECT default_password_hash FROM study_groups WHERE id=?", groupID).Scan(&hash)
	return hash, err
}

func (a *app) createUserWithHash(username, displayName, namePinyin, hash string, isSuper bool, actorID uint64) (uint64, error) {
	username = normalizeUsername(firstNonEmpty(username, namePinyin, displayName))
	if username == "" || displayName == "" {
		return 0, errors.New("username_display_name_required")
	}
	now := nowSQL()
	res, err := a.db.Exec(`INSERT INTO users (username,display_name,name_pinyin,password_hash,is_super_admin,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)`, username, displayName, firstNonEmpty(namePinyin, username), hash, isSuper, actorID, now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func (a *app) addMember(groupID, userID uint64, memberName string, actorID uint64) error {
	_, err := a.db.Exec(`INSERT INTO group_members (group_id,user_id,member_name,joined_at,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE status=1, updated_at=VALUES(updated_at)`, groupID, userID, memberName, nowSQL(), actorID, nowSQL(), nowSQL())
	return err
}

func addMemberTx(tx *sql.Tx, groupID, userID uint64, memberName string, actorID uint64) error {
	_, err := tx.Exec(`INSERT INTO group_members (group_id,user_id,member_name,joined_at,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE status=1, updated_at=VALUES(updated_at)`, groupID, userID, memberName, nowSQL(), actorID, nowSQL(), nowSQL())
	return err
}

func (a *app) audit(groupID, actorID uint64, action, targetType string, targetID uint64, before, after any, r *http.Request) {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	_, _ = a.db.Exec(`INSERT INTO audit_logs (group_id,actor_user_id,action,target_type,target_id,before_json,after_json,ip,user_agent,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		nullableID(groupID), actorID, action, targetType, nullableID(targetID), nullJSON(beforeJSON), nullJSON(afterJSON), clientIP(r), r.UserAgent(), nowSQL())
}

func (a *app) signToken(c tokenClaims) (string, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body64 := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(body64))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body64 + "." + sig, nil
}

func newTokenClaims(userID, currentGroupID, authVersion uint64) tokenClaims {
	return tokenClaims{
		UserID:         userID,
		CurrentGroupID: currentGroupID,
		AuthVersion:    authVersion,
		ExpiresAt:      time.Now().Add(tokenTTL).Unix(),
	}
}

func (a *app) verifyToken(token string) (tokenClaims, error) {
	var c tokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, errors.New("invalid_token")
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(expected, got) {
		return c, errors.New("invalid_token")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return c, err
	}
	if c.UserID == 0 || c.AuthVersion == 0 || c.ExpiresAt <= time.Now().Unix() {
		return c, errors.New("expired")
	}
	return c, nil
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk := pbkdf2Key([]byte(password), salt, 120000, 32, sha256.New)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(dk), nil
}

func verifyPassword(password, stored string) bool {
	parts := strings.Split(stored, ":")
	if len(parts) != 2 {
		return false
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	got := pbkdf2Key([]byte(password), salt, 120000, len(want), sha256.New)
	return hmac.Equal(want, got)
}

func pbkdf2Key(password, salt []byte, iter, keyLen int, h func() hash.Hash) []byte {
	prf := hmac.New(h, password)
	hashLen := prf.Size()
	numBlocks := int(math.Ceil(float64(keyLen) / float64(hashLen)))
	var dk []byte
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		prf.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := prf.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for x := range t {
				t[x] ^= u[x]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

type loginLimiter struct {
	mu       sync.Mutex
	failures map[string]loginFailure
}

type loginFailure struct {
	Count     int
	BlockedTo time.Time
	LastSeen  time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{failures: map[string]loginFailure{}}
}

func (l *loginLimiter) key(ip, username string) string {
	return ip + "|" + username
}

func (l *loginLimiter) blocked(ip, username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := l.key(ip, username)
	item, ok := l.failures[key]
	if !ok {
		return false
	}
	now := time.Now()
	if (!item.LastSeen.IsZero() && now.Sub(item.LastSeen) > loginFailureTTL) ||
		(!item.BlockedTo.IsZero() && !item.BlockedTo.After(now)) {
		delete(l.failures, key)
		return false
	}
	return item.BlockedTo.After(now)
}

func (l *loginLimiter) fail(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := l.key(ip, username)
	item := l.failures[key]
	now := time.Now()
	item.Count++
	item.LastSeen = now
	if item.Count >= 8 {
		item.BlockedTo = now.Add(10 * time.Minute)
	}
	l.failures[key] = item
	if len(l.failures) > loginFailureCap {
		l.pruneLocked(now)
	}
}

func (l *loginLimiter) pruneLocked(now time.Time) {
	for key, failure := range l.failures {
		if failure.LastSeen.IsZero() || now.Sub(failure.LastSeen) > loginFailureTTL ||
			(!failure.BlockedTo.IsZero() && !failure.BlockedTo.After(now)) {
			delete(l.failures, key)
		}
	}
	for len(l.failures) > loginFailureCap {
		oldestKey := ""
		var oldest time.Time
		for key, failure := range l.failures {
			if oldestKey == "" || failure.LastSeen.Before(oldest) {
				oldestKey = key
				oldest = failure.LastSeen
			}
		}
		if oldestKey == "" {
			break
		}
		delete(l.failures, oldestKey)
	}
}

func (l *loginLimiter) success(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, l.key(ip, username))
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return readJSONLimit(w, r, v, 1<<20)
}

func readJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil || int64(len(body)) > limit || json.Unmarshal(body, v) != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code})
}

func nowSQL() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05.000")
}

func nullableID(id uint64) any {
	if id == 0 {
		return nil
	}
	return id
}

func nullableUint64(v sql.NullInt64) any {
	if !v.Valid || v.Int64 <= 0 {
		return nil
	}
	return uint64(v.Int64)
}

func nullJSON(b []byte) any {
	if string(b) == "null" || len(b) == 0 {
		return nil
	}
	return string(b)
}

func queryDate(r *http.Request, key string, fallback time.Time) string {
	v := r.URL.Query().Get(key)
	if _, err := time.Parse("2006-01-02", v); err == nil {
		return v
	}
	return fallback.Format("2006-01-02")
}

func queryInt(r *http.Request, key string, fallback int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return fallback
	}
	return v
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func containsGroup(groups []group, id uint64) bool {
	for _, g := range groups {
		if g.ID == id {
			return true
		}
	}
	return false
}

func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeUsername(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= n {
		return string(rs)
	}
	return string(rs[:n])
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func randomPassword(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "Agp" + strconv.FormatInt(time.Now().Unix()%100000, 10)
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf)
}
