#!/bin/sh
set -eu

DIR="${1:-/volume1/web/poems/Newtestament}"
MODE="${2:---dry-run}"

if [ ! -d "$DIR" ]; then
  echo "目录不存在: $DIR" >&2
  echo "用法: sh $0 /volume1/web/poems/Newtestament [--dry-run|--apply]" >&2
  exit 1
fi

if [ "$MODE" != "--dry-run" ] && [ "$MODE" != "--apply" ]; then
  echo "第二个参数只能是 --dry-run 或 --apply" >&2
  exit 1
fi

renamed=0
skipped=0

rename_largest_match() {
  seq="$1"
  keyword="$2"
  target="$3.mp4"

  if [ -e "$DIR/$target" ]; then
    printf '跳过（目标已存在）: %s\n' "$target"
    skipped=$((skipped + 1))
    return
  fi

  best=""
  best_size=0
  for file in "$DIR"/*-"$seq"-*"$keyword"*.mp4 "$DIR"/*-"$seq"-*"$keyword"*.MP4; do
    [ -f "$file" ] || continue
    size=$(wc -c < "$file" | tr -d ' ')
    if [ "$size" -gt "$best_size" ]; then
      best="$file"
      best_size="$size"
    fi
  done

  if [ -z "$best" ]; then
    printf '不处理（未识别）: 第 %s 课 / %s\n' "$seq" "$keyword"
    skipped=$((skipped + 1))
    return
  fi

  if [ "$MODE" = "--apply" ]; then
    mv -- "$best" "$DIR/$target"
    printf '已重命名: %s -> %s\n' "$(basename "$best")" "$target"
  else
    printf '预览: %s -> %s\n' "$(basename "$best")" "$target"
  fi
  renamed=$((renamed + 1))
}

# 目标名沿用当前目录已经使用的命名风格；A/B/C/D 对应上/下或第 1/2/3/4 部分。
rename_largest_match 01 "圣经引言" "prefaceA"
rename_largest_match 02 "圣经引言" "prefaceB"
rename_largest_match 03 "马太福音" "mattewA"
rename_largest_match 04 "马太福音" "mattewB"
rename_largest_match 05 "使徒行传" "actsA"
rename_largest_match 06 "使徒行传" "actsB"
rename_largest_match 07 "马可福音" "markA"
rename_largest_match 08 "马可福音" "markB"
rename_largest_match 09 "路加福音" "lukeA"
rename_largest_match 10 "路加福音" "lukeB"
rename_largest_match 11 "约翰福音" "johnA"
rename_largest_match 12 "约翰福音" "johnB"
rename_largest_match 13 "四福音合参" "gospelsA"
rename_largest_match 14 "四福音合参" "gospelsB"
rename_largest_match 15 "保罗书信纵览" "PaulLetterA"
rename_largest_match 16 "保罗书信纵览" "PaulLetterB"
rename_largest_match 17 "帖前" "thessaloniansA"
rename_largest_match 18 "帖前" "thessaloniansB"
rename_largest_match 19 "帖前" "thessaloniansC"
rename_largest_match 20 "帖后" "thessaloniansD"
rename_largest_match 21 "林前" "corinthiansA"
rename_largest_match 22 "林前" "corinthiansB"
rename_largest_match 23 "林后" "corinthians2A"
rename_largest_match 24 "林后" "corinthians2B"
rename_largest_match 25 "林后" "corinthians2C"
rename_largest_match 26 "林后" "corinthians2D"
rename_largest_match 27 "加拉太书" "galatiansA"
rename_largest_match 28 "加拉太书" "galatiansB"
rename_largest_match 29 "加拉太书" "galatiansC"
rename_largest_match 30 "罗马书" "romansA"
rename_largest_match 31 "罗马书" "romansB"
rename_largest_match 32 "歌罗西书" "colossians"
rename_largest_match 33 "以弗所书" "ephesians"
rename_largest_match 34 "腓立比书" "philippiansA"
rename_largest_match 35 "腓立比书" "philippiansB"
rename_largest_match 36 "腓利门书" "philemon"
rename_largest_match 37 "保罗第四组书信" "PaulLetter2"
rename_largest_match 38 "提摩太前书" "timothy1"
rename_largest_match 39 "提多书" "titus"
rename_largest_match 40 "提摩太后书" "timothy2"
rename_largest_match 41 "希伯来书" "hebrewsA"
rename_largest_match 42 "希伯来书" "hebrewsB"
rename_largest_match 43 "彼得前书" "Peter1"
rename_largest_match 44 "彼得后书" "Peter2"
rename_largest_match 45 "雅各书" "james"
rename_largest_match 46 "约翰壹书" "johnLetter1A"
rename_largest_match 47 "约翰壹书" "johnLetter1B"
rename_largest_match 48 "约翰贰叁书" "johnLetter23"
rename_largest_match 49 "犹大书" "jude"
rename_largest_match 50 "启示录" "revelationA"
rename_largest_match 51 "启示录" "revelationB"

printf '\n完成：%s %s，跳过 %s。\n' "$renamed" "$( [ "$MODE" = "--apply" ] && echo '个重命名' || echo '个待重命名预览' )" "$skipped"
if [ "$MODE" = "--dry-run" ]; then
  echo "以上只是预览；确认无误后，将第二个参数改为 --apply。"
fi
