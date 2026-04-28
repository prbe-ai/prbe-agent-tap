#!/usr/bin/env bash
set -euo pipefail

REPO="prbe-ai/prbe-agent-tap"
LATEST_BASE="https://github.com/${REPO}/releases/latest/download"

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux)  echo "linux" ;;
    *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    arm64|aarch64) echo "arm64" ;;
    x86_64|amd64)  echo "amd64" ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
  esac
}

install_dir() {
  if [ -w "/usr/local/bin" ]; then
    echo "/usr/local/bin"
  else
    mkdir -p "$HOME/.local/bin"
    echo "$HOME/.local/bin"
  fi
}

main() {
  local os arch dir url tmp bin
  os="$(detect_os)"
  arch="$(detect_arch)"
  dir="$(install_dir)"
  url="${LATEST_BASE}/prbe-agent-tap-${os}-${arch}"
  tmp="$(mktemp)"
  echo "Downloading ${url}"
  curl -fsSL --retry 3 -o "${tmp}" "${url}"
  chmod +x "${tmp}"
  bin="${dir}/prbe-agent-tap"
  mv "${tmp}" "${bin}"
  echo "Installed ${bin}"

  echo
  printf "Paste your pairing token from the dashboard: "
  if [ -t 0 ]; then
    IFS= read -r token
  elif [ -e /dev/tty ]; then
    IFS= read -r token </dev/tty
  else
    echo "no TTY available; rerun and pass the token: ${bin} pair <token>" >&2
    token=""
  fi
  if [ -z "${token}" ]; then
    echo "no token provided; you can pair later with: ${bin} pair <token>" >&2
    exit 0
  fi

  "${bin}" pair "${token}"
  "${bin}" install
  "${bin}" status
}

main "$@"
