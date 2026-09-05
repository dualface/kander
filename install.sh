#!/bin/sh

set -eu

source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

kander_lang=
kander_lang_set=0
project_arg=
project_set=0
show_help=0
parse_error=0
missing_lang=0
missing_project=0
duplicate_project=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --lang)
      if [ "$#" -lt 2 ]; then
        missing_lang=1
        parse_error=1
        break
      fi
      kander_lang_set=1
      kander_lang=$2
      shift 2
      ;;
    --lang=*)
      kander_lang_set=1
      kander_lang=${1#--lang=}
      shift
      ;;
    --project)
      if [ "$project_set" -eq 1 ]; then
        duplicate_project=1
        parse_error=1
        break
      fi
      if [ "$#" -lt 2 ]; then
        missing_project=1
        parse_error=1
        break
      fi
      case "$2" in
        --lang|--lang=*|--project|--project=*|-h|--help)
          missing_project=1
          parse_error=1
          break
          ;;
      esac
      project_set=1
      project_arg=$2
      shift 2
      ;;
    --project=*)
      if [ "$project_set" -eq 1 ]; then
        duplicate_project=1
        parse_error=1
        break
      fi
      project_set=1
      project_arg=${1#--project=}
      shift
      ;;
    -h|--help)
      show_help=1
      shift
      ;;
    *)
      parse_error=1
      break
      ;;
  esac
done

if [ "$project_set" -eq 1 ] && [ -z "$project_arg" ]; then
  missing_project=1
  parse_error=1
fi

kander_locale=
case "$kander_lang" in
  cn) kander_locale=cn ;;
  en) kander_locale=en ;;
esac
if [ -z "$kander_locale" ]; then
  kander_locale=${KANDER_LANG:-${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}}
fi
case "$(printf '%s' "$kander_locale" | tr '[:upper:]' '[:lower:]')" in
  en*) kander_zh=0 ;;
  *) kander_zh=1 ;;
esac

usage() {
  if [ "$kander_zh" -eq 1 ]; then
    echo "用法: install.sh [--lang {cn,en}] [--project <目录>]"
    echo "把 kander 装到 ~/.local/bin, 规则装到 ~/.agents, 资源装到 ~/.local/share/kander."
    echo "指定 --project 时只装到该 Git 项目主 worktree 的 .kander/, 不写全局路径."
  else
    echo "usage: install.sh [--lang {cn,en}] [--project <directory>]"
    echo "Install kander to ~/.local/bin, rules to ~/.agents, and assets to ~/.local/share/kander."
    echo "With --project, install only into that Git project's main worktree .kander/ and skip global paths."
  fi
}

fail_usage() {
  usage >&2
  if [ "$#" -gt 0 ]; then
    printf '%s\n' "$1" >&2
  fi
  exit 2
}

if [ "$missing_lang" -eq 1 ] || {
  [ "$kander_lang_set" -eq 1 ] && [ "$kander_lang" != "cn" ] && [ "$kander_lang" != "en" ]
}; then
  if [ "$kander_zh" -eq 1 ]; then
    fail_usage "错误: --lang 只接受 cn 或 en"
  else
    fail_usage "error: --lang must be cn or en"
  fi
fi
if [ "$missing_project" -eq 1 ]; then
  if [ "$kander_zh" -eq 1 ]; then
    fail_usage "错误: --project 需要目录"
  else
    fail_usage "error: --project requires a directory"
  fi
fi
if [ "$duplicate_project" -eq 1 ]; then
  if [ "$kander_zh" -eq 1 ]; then
    fail_usage "错误: --project 只能指定一次"
  else
    fail_usage "error: --project may be given only once"
  fi
fi
if [ "$show_help" -eq 1 ] && [ "$parse_error" -eq 0 ]; then
  usage
  exit 0
fi
if [ "$parse_error" -eq 1 ]; then
  usage >&2
  exit 2
fi

error_msg() {
  if [ "$kander_zh" -eq 1 ]; then
    printf '%s\n' "$1" >&2
  else
    printf '%s\n' "$2" >&2
  fi
}

fail() {
  error_msg "$1" "$2"
  exit 1
}

reject_if_directory() {
  target=$1
  if [ -d "$target" ]; then
    fail "错误: 安装目标是目录: $target" "error: installation target is a directory: $target"
  fi
}

reject_if_symlink() {
  target=$1
  if [ -L "$target" ]; then
    fail "错误: 安装目标是符号链接: $target" "error: installation target is a symlink: $target"
  fi
}

reject_if_not_dir() {
  target=$1
  if [ -e "$target" ] || [ -L "$target" ]; then
    reject_if_symlink "$target"
    if [ ! -d "$target" ]; then
      fail "错误: 安装目标不是目录: $target" "error: installation target is not a directory: $target"
    fi
  fi
}

git_cmd() {
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_COMMON_DIR git "$@"
}

ensure_binary() {
  if [ -L "$source_dir/kander" ]; then
    fail "错误: 安装目标是符号链接: $source_dir/kander" "error: installation target is a symlink: $source_dir/kander"
  fi
  if [ -d "$source_dir/kander" ]; then
    fail "错误: 安装目标是目录: $source_dir/kander" "error: installation target is a directory: $source_dir/kander"
  fi
  if [ -f "$source_dir/kander" ] && [ -x "$source_dir/kander" ]; then
    printf '%s\n' "$source_dir/kander"
    return 0
  fi
  if ! command -v go >/dev/null 2>&1; then
    fail "错误: 未找到已构建的 kander, 且 go 不可用" "error: built kander is missing and go is unavailable"
  fi
  build_timestamp=$(date -u +%Y%m%dT%H%M%SZ)
  git_hash=$(git_cmd -C "$source_dir" rev-parse --short=12 HEAD 2>/dev/null || true)
  if [ -z "$git_hash" ]; then
    git_hash=unknown
  fi
  version_package=github.com/dualface/kander/internal/version
  version_ldflags="-X ${version_package}.BuildTimestamp=${build_timestamp} -X ${version_package}.GitHash=${git_hash}"
  (CDPATH= cd -- "$source_dir" && go build -ldflags "$version_ldflags" -o kander ./cmd/kander) || \
    fail "错误: go build 失败" "error: go build failed"
  printf '%s\n' "$source_dir/kander"
}

share_rel_paths() {
  share_paths=
  if [ ! -d "$source_dir/share" ]; then
    return 0
  fi
  share_paths=$(CDPATH= cd -- "$source_dir/share" && find . -type f | sed 's|^\./||')
}

reject_payload_targets() {
  dest_bin=$1
  dest_agents=$2
  dest_share=$3
  reject_symlinks=${4:-0}
  if [ "$reject_symlinks" -eq 1 ]; then
    reject_if_symlink "$dest_bin/kander"
  fi
  reject_if_directory "$dest_bin/kander"
  for rule in "$source_dir"/rules/*.md; do
    [ -f "$rule" ] || continue
    if [ "$reject_symlinks" -eq 1 ]; then
      reject_if_symlink "$dest_agents/$(basename "$rule")"
    fi
    reject_if_directory "$dest_agents/$(basename "$rule")"
  done
  if [ -d "$source_dir/share" ]; then
    reject_if_not_dir "$dest_share"
    for rel in $share_paths; do
      [ -n "$rel" ] || continue
      dest="$dest_share/$rel"
      parent=$(dirname -- "$dest")
      if [ "$parent" != "$dest_share" ]; then
        reject_if_not_dir "$parent"
      fi
      if [ "$reject_symlinks" -eq 1 ]; then
        reject_if_symlink "$dest"
      fi
      reject_if_directory "$dest"
    done
  fi
}

install_payloads() {
  dest_bin=$1
  dest_agents=$2
  dest_share=$3
  binary=$4
  mkdir -p "$dest_bin" "$dest_agents"
  install -m 0755 "$binary" "$dest_bin/kander"
  for rule in "$source_dir"/rules/*.md; do
    [ -f "$rule" ] || continue
    install -m 0644 "$rule" "$dest_agents/$(basename "$rule")"
  done
  if [ -d "$source_dir/share" ]; then
    for rel in $share_paths; do
      [ -n "$rel" ] || continue
      dest="$dest_share/$rel"
      mkdir -p "$(dirname -- "$dest")"
      install -m 0644 "$source_dir/share/$rel" "$dest"
    done
  fi
  agent_rules="$dest_agents/AGENTS.md"
  entry_rules="$dest_agents/KANDER-AGENTS.md"
  if [ -f "$entry_rules" ] && [ ! -e "$agent_rules" ] && [ ! -L "$agent_rules" ]; then
    ln -s "$(basename "$entry_rules")" "$agent_rules"
  fi
}

print_installed() {
  if [ "$kander_zh" -eq 1 ]; then
    printf '%s\n' 'Kander 已安装'
  else
    printf '%s\n' 'Kander installed'
  fi
}

resolve_project_paths() {
  target=$1
  if [ -z "$target" ]; then
    fail "错误: 项目目录不存在: $target" "error: project directory does not exist: $target"
  fi
  if [ -L "$target" ]; then
    fail "错误: 路径分量不得是符号链接/重解析点: $target" "error: path component must not be a symlink/reparse point: $target"
  fi
  if [ ! -d "$target" ]; then
    fail "错误: 项目目录不存在: $target" "error: project directory does not exist: $target"
  fi
  abs=$(CDPATH= cd -- "$target" && pwd)
  if [ -L "$abs" ]; then
    fail "错误: 路径分量不得是符号链接/重解析点: $abs" "error: path component must not be a symlink/reparse point: $abs"
  fi
  main=$(git_cmd -C "$abs" worktree list --porcelain 2>/dev/null | sed -n '1s/^worktree //p' || true)
  if [ -z "$main" ]; then
    fail "错误: 项目不是 Git 仓库: $abs" "error: project is not a Git repository: $abs"
  fi
  if [ -L "$main" ]; then
    fail "错误: 路径分量不得是符号链接/重解析点: $main" "error: path component must not be a symlink/reparse point: $main"
  fi
  install_root="$main/.kander"
  printf '%s\n' "$main"
  printf '%s\n' "$install_root"
  printf '%s\n' "$install_root/bin"
  printf '%s\n' "$install_root/rules"
  printf '%s\n' "$install_root/share"
}

reject_existing_dir() {
  path=$1
  if [ -e "$path" ] || [ -L "$path" ]; then
    reject_if_symlink "$path"
    if [ ! -d "$path" ]; then
      fail "错误: 安装目标不是目录: $path" "error: installation target is not a directory: $path"
    fi
  fi
}

append_git_exclude() {
  git_root=$1
  if [ -L "$git_root" ]; then
    fail "错误: 路径分量不得是符号链接/重解析点: $git_root" "error: path component must not be a symlink/reparse point: $git_root"
  fi
  exclude=$(git_cmd -C "$git_root" rev-parse --git-path info/exclude)
  case "$exclude" in
    /*) ;;
    *) exclude="$git_root/$exclude" ;;
  esac
  parent=$(dirname -- "$exclude")
  current=$git_root
  rest=${parent#"$git_root"}
  rest=${rest#/}
  if [ "$parent" != "$git_root" ] && [ -n "$rest" ]; then
    old_ifs=$IFS
    IFS=/
    set -- $rest
    IFS=$old_ifs
    for part in "$@"; do
      [ -n "$part" ] || continue
      current="$current/$part"
      if [ -L "$current" ]; then
        fail "错误: 路径分量不得是符号链接/重解析点: $current" "error: path component must not be a symlink/reparse point: $current"
      fi
    done
  fi
  if [ -L "$exclude" ]; then
    fail "错误: 路径分量不得是符号链接/重解析点: $exclude" "error: path component must not be a symlink/reparse point: $exclude"
  fi
  if [ -d "$exclude" ]; then
    fail "错误: 安装目标是目录: $exclude" "error: installation target is a directory: $exclude"
  fi
  mkdir -p "$parent"
  if [ -f "$exclude" ]; then
    if grep -Fqx '/.kander/' "$exclude"; then
      return 0
    fi
    if [ -s "$exclude" ] && [ "$(tail -c 1 "$exclude" | wc -l)" -eq 0 ]; then
      printf '\n' >> "$exclude"
    fi
    printf '%s\n' '/.kander/' >> "$exclude"
  else
    printf '%s\n' '/.kander/' > "$exclude"
  fi
}

binary=$(ensure_binary)
share_rel_paths

if [ "$project_set" -eq 1 ]; then
  prepared=$(resolve_project_paths "$project_arg")
  {
    IFS= read -r _main_worktree
    IFS= read -r _install_target
    IFS= read -r dest_bin
    IFS= read -r dest_agents
    IFS= read -r dest_share
  } <<EOF
$prepared
EOF
  if [ -z "${dest_bin:-}" ] || [ -z "${dest_agents:-}" ]; then
    fail "错误: 无法解析项目安装路径" "error: failed to resolve project install paths"
  fi
  reject_existing_dir "$_install_target"
  reject_existing_dir "$dest_bin"
  reject_existing_dir "$dest_agents"
  reject_existing_dir "$dest_share"
  reject_payload_targets "$dest_bin" "$dest_agents" "$dest_share" 1
  append_git_exclude "$_main_worktree"
  install_payloads "$dest_bin" "$dest_agents" "$dest_share" "$binary"
  print_installed
  printf '%s\n' "$dest_bin/kander"
  if [ "$kander_zh" -eq 1 ]; then
    printf '%s\n' \
      "项目安装完成, 未修改 PATH, 也未改动全局 Kander 安装." \
      "请使用以上绝对路径." \
      >&2
  else
    printf '%s\n' \
      "Project install finished; PATH and the global Kander install were not changed." \
      "Use the absolute command path above." \
      >&2
  fi
  exit 0
fi

bin_dir="$HOME/.local/bin"
agents_dir="$HOME/.agents"
share_dir="$HOME/.local/share/kander"
legacy_commands=
remove_legacy=0

reject_payload_targets "$bin_dir" "$agents_dir" "$share_dir"
for legacy_command in onevoke kanban onevoke-review.sh onevoke-review; do
  target="$bin_dir/$legacy_command"
  if [ -d "$target" ]; then
    fail "错误: 旧版安装目标是目录: $target" "error: legacy installation target is a directory: $target"
  fi
  if [ -e "$target" ] || [ -L "$target" ]; then
    legacy_commands="${legacy_commands}${legacy_commands:+ }$legacy_command"
  fi
done

if [ -n "$legacy_commands" ]; then
  if [ "$kander_zh" -eq 1 ]; then
    printf '%s\n' \
      "检测到已退役的 onevoke/kanban 入口:" \
      "  $legacy_commands" \
      "命令入口现已统一为 kander." \
      >&2
    printf '%s' "是否删除这些旧入口? [y/N] " >&2
  else
    printf '%s\n' \
      "Retired onevoke/kanban entries were detected:" \
      "  $legacy_commands" \
      "The command entry point is now unified as kander." \
      >&2
    printf '%s' "Delete these legacy entries? [y/N] " >&2
  fi
  legacy_answer=
  if IFS= read -r legacy_answer; then
    :
  fi
  if [ ! -t 0 ]; then
    printf '\n' >&2
  fi
  case "$legacy_answer" in
    y|Y|yes|YES|Yes|是)
      remove_legacy=1
      ;;
    *)
      if [ "$kander_zh" -eq 1 ]; then
        printf '%s\n' "已保留旧入口." >&2
      else
        printf '%s\n' "Legacy entries were kept." >&2
      fi
      ;;
  esac
fi

install_payloads "$bin_dir" "$agents_dir" "$share_dir" "$binary"

if [ "$remove_legacy" -eq 1 ]; then
  if [ ! -x "$bin_dir/kander" ]; then
    fail "错误: 新入口不可执行, 已保留旧入口: $bin_dir/kander" "error: new entry is not executable; legacy entries were kept: $bin_dir/kander"
  fi
  for legacy_command in $legacy_commands; do
    rm -f "$bin_dir/$legacy_command"
  done
  if [ "$kander_zh" -eq 1 ]; then
    printf '%s\n' "已删除旧入口." >&2
  else
    printf '%s\n' "Legacy entries were removed." >&2
  fi
fi

print_installed
exit 0
