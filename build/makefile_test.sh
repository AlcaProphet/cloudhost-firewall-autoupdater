#!/bin/sh
# 在隔离目录用本地命令替身检查真实 Makefile，不安装依赖或生成占位 dist。
# 可选参数指定待验证的 Makefile，便于在仓库外运行旧版或变异负向控制。
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source_makefile=${1:-"$script_dir/../Makefile"}
case "$source_makefile" in
    /*) ;;
    *) source_makefile="$PWD/$source_makefile" ;;
esac
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/fwalizer-makefile-test.XXXXXX")
trap 'rm -rf "$tmp_dir"' 0
trap 'exit 1' HUP INT TERM
mkdir "$tmp_dir/bin"

cat > "$tmp_dir/bin/npm" <<'SH'
#!/bin/sh
set -eu
case "$*" in
    ci)
        printf '%s\n' 'npm ci' >> "$TRACE"
        if [ "$FAIL_STAGE" = ci ]; then exit 17; fi
        ;;
    'run build')
        printf '%s\n' 'npm run build' >> "$TRACE"
        if [ "$FAIL_STAGE" = frontend-build ]; then exit 17; fi
        # 延迟使只给 all 加兄弟前置的错误方案在并行时暴露提前执行。
        sleep 1
        : > "$READY"
        ;;
    *) exit 29 ;;
esac
SH
cat > "$tmp_dir/bin/go" <<'SH'
#!/bin/sh
set -eu
printf 'go %s\n' "$1" >> "$TRACE"
if [ ! -f "$READY" ]; then
    printf '%s\n' 'go-before-frontend' >> "$TRACE"
    exit 19
fi
if [ "$FAIL_STAGE" = "$1" ]; then exit 23; fi
case "$1" in test|vet|build) ;; *) exit 29 ;; esac
SH
chmod +x "$tmp_dir/bin/npm" "$tmp_dir/bin/go"

case_count=0
run_case() {
    label=$1
    failure=$2
    expected_go=$3
    shift 3
    case_dir="$tmp_dir/$label"
    mkdir -p "$case_dir/webui/frontend"
    cp "$source_makefile" "$case_dir/Makefile"
    # 同名文件不能让 .PHONY frontend 被误判为已经准备完成。
    : > "$case_dir/frontend"
    : > "$case_dir/trace"
    if (
        cd "$case_dir"
        PATH="$tmp_dir/bin:$PATH"
        TRACE="$case_dir/trace"
        READY="$case_dir/frontend-complete"
        FAIL_STAGE=$failure
        export PATH TRACE READY FAIL_STAGE
        make "$@"
    ) > "$case_dir/output" 2>&1; then
        result=0
    else
        result=$?
    fi

    valid=1
    if [ -z "$failure" ]; then
        if [ "$result" -ne 0 ]; then valid=0; fi
    elif [ "$result" -eq 0 ]; then
        valid=0
    fi
    # 每次调用共享一次安装/构建；前端失败不执行 Go，Go 失败也必须传出。
    if ! awk -v failure="$failure" -v expected_go="$expected_go" -v goal="$1" '
        $0 == "npm ci" { ci++ }
        $0 == "npm run build" { frontend++ }
        $1 == "go" { go++; called[$2]++ }
        $0 == "go-before-frontend" { early++ }
        END {
            if (ci != 1 || early) exit 1
            if (frontend != (failure == "ci" ? 0 : 1)) exit 1
            if (expected_go >= 0 && go != expected_go) exit 1
            if (expected_go == 1 && called[goal] != 1) exit 1
            if (expected_go == 3 && (called["test"] != 1 || called["vet"] != 1 || called["build"] != 1)) exit 1
            if (failure ~ /^(test|vet|build)$/ && !called[failure]) exit 1
        }
    ' "$case_dir/trace"; then
        valid=0
    fi
    if [ "$valid" -ne 1 ]; then
        printf 'FAIL: %s (exit=%s)\n' "$label" "$result" >&2
        cat "$case_dir/trace" "$case_dir/output" >&2
        exit 1
    fi
    case_count=$((case_count + 1))
    printf 'PASS: %s\n' "$label"
}

run_case test '' 1 test
run_case vet '' 1 vet
run_case build '' 1 build
run_case all '' 3 all
run_case parallel-all '' 3 -j4 all
run_case parallel-multiple-goals '' 3 -j4 vet test build
for stage in ci frontend-build; do
    for goal in test vet build all; do
        run_case "fail-$stage-$goal" "$stage" 0 -j4 "$goal"
    done
done
for stage in vet test build; do
    run_case "fail-go-$stage" "$stage" -1 -j4 all
done
printf 'Makefile 回归通过：%s 项\n' "$case_count"
