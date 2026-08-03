#!/usr/bin/env sh

install_go_tool() {
  go_tool_path="$1"
  go_tool_package="$2"
  if [ -x "$go_tool_path" ]; then
    return
  fi
  go_tool_dir="$(dirname "$go_tool_path")"
  mkdir -p "$go_tool_dir"
  GOBIN="$go_tool_dir" go install "$go_tool_package"
}
