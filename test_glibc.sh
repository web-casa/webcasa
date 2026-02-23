#!/bin/bash
GLIBC_VER="0.0"
if command -v ldd &>/dev/null; then
    RAW_VER=$(ldd --version 2>&1 | awk 'NR==1 {print $NF}')
    if [[ "$RAW_VER" =~ ^[0-9]+\.[0-9]+ ]]; then
        GLIBC_VER="$RAW_VER"
    fi
fi
echo "$GLIBC_VER"
