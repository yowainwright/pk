# pk terminal lifecycle plugin for zsh.
[[ -o interactive ]] || return 0
[[ -n "${PK_DISABLE_SESSION:-}" ]] && return 0
[[ -n "${PK_TERMINAL_SESSION_ID:-}" ]] && return 0

typeset -g _pk_bin="__PK_EXECUTABLE__"
[[ -x "$_pk_bin" ]] || return 0

autoload -Uz add-zsh-hook || return 0

PK_TERMINAL_SESSION_ID="$("$_pk_bin" __session-id 2>/dev/null)"
export PK_TERMINAL_SESSION_ID

[[ -n "${PK_TERMINAL_SESSION_ID:-}" ]] || return 0

typeset -g _pk_command_active=0
typeset -g _pk_tab_id="${PK_TAB_ID:-${ITERM_SESSION_ID:-${TERM_SESSION_ID:-}}}"
typeset -g _pk_window_id="${PK_WINDOW_ID:-}"
typeset -g _pk_agent_session_id="${PK_AGENT_SESSION_ID:-}"
typeset -g _pk_user_session_id="${PK_USER_SESSION_ID:-}"

if [[ -z "$_pk_window_id" && -n "${ITERM_SESSION_ID:-}" ]]; then
  _pk_window_id="${ITERM_SESSION_ID%%t*}"
fi

_pk_optional_arg() {
  local name="$1"
  local value="$2"
  [[ -n "$value" ]] || return 0
  args+=("$name" "$value")
}

_pk_emit() {
  local kind="$1"
  local exit_code="${2:-}"
  local args
  args=(
    __session
    --kind "$kind"
    --source zsh
    --terminal-session-id "$PK_TERMINAL_SESSION_ID"
    --shell-pid "$$"
    --parent-pid "$PPID"
    --cwd "$PWD"
  )
  _pk_optional_arg --tab-id "$_pk_tab_id"
  _pk_optional_arg --window-id "$_pk_window_id"
  _pk_optional_arg --agent-session-id "$_pk_agent_session_id"
  _pk_optional_arg --user-session-id "$_pk_user_session_id"
  if [[ -n "$exit_code" ]]; then
    args+=(--exit-code "$exit_code")
  fi
  "$_pk_bin" "${args[@]}" >/dev/null 2>&1
}

_pk_preexec() {
  _pk_command_active=1
  _pk_emit command.start
}

_pk_precmd() {
  local status="$?"
  if [[ "$_pk_command_active" = "1" ]]; then
    _pk_emit command.finish "$status"
    _pk_command_active=0
  fi
  _pk_emit session.inactive
}

_pk_zshexit() {
  _pk_emit session.stop
}

_pk_emit session.start
add-zsh-hook preexec _pk_preexec
add-zsh-hook precmd _pk_precmd
add-zsh-hook zshexit _pk_zshexit
