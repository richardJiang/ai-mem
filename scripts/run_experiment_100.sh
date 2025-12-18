#!/bin/bash
set -euo pipefail

HOST=${HOST:-http://localhost:8080}
RUNS=${RUNS:-100}
SEED=${SEED:-0}
TASK_TYPE=${TASK_TYPE:-lottery}
ACTION=${ACTION:-lottery}
RULE_MODE=${RULE_MODE:-none}

body=$(cat <<JSON
{
  "task_type": "${TASK_TYPE}",
  "runs_per_group": ${RUNS},
  "groups": ["A","B","C"],
  "seed": ${SEED},
  "action": "${ACTION}",
  "rule_mode": "${RULE_MODE}"
}
JSON
)

echo "[experiment] POST ${HOST}/api/experiments/run"
tmp_body="$(mktemp)"
http_code=$(curl -sS -o "${tmp_body}" -w "%{http_code}" -X POST "${HOST}/api/experiments/run" \
  -H 'Content-Type: application/json' \
  -d "${body}" || true)

resp="$(cat "${tmp_body}" 2>/dev/null || true)"
rm -f "${tmp_body}"

resp_trim="$(printf '%s' "${resp}" | tr -d '\r\n\t ')"

if [[ "${http_code}" != "200" ]] || [[ -z "${resp_trim}" ]]; then
  echo "[experiment] 请求失败或返回非JSON"
  echo "HTTP_CODE: ${http_code}"
  echo "RAW_RESPONSE_BEGIN"
  echo "${resp}"
  echo "RAW_RESPONSE_END"
  echo ""
  echo "排查建议："
  echo "1) 确认后端已启动且可访问：curl ${HOST}/api/memories"
  echo "2) 查看后端日志是否有 500 报错（尤其是 /api/experiments/run）"
  exit 1
fi

echo "$resp" | python3 - <<'PY'
import json,sys
s=sys.stdin.read()
ss=s.strip()
if not ss:
    print("[experiment] 返回体为空/仅空白，无法解析 JSON")
    sys.exit(1)
try:
    obj=json.loads(ss)
except Exception as e:
    print("[experiment] 返回体不是合法 JSON，解析失败：", repr(e))
    print("RAW_RESPONSE_BEGIN")
    print(s)
    print("RAW_RESPONSE_END")
    sys.exit(1)

res=obj.get('result',{}) if isinstance(obj, dict) else {}
print('run_id:', res.get('run_id') if isinstance(res, dict) else None)
print('result_path:', (obj.get('result_path') if isinstance(obj, dict) else None) or (res.get('result_path') if isinstance(res, dict) else None))
print('conclusion_path:', (obj.get('conclusion_path') if isinstance(obj, dict) else None) or (res.get('conclusion_path') if isinstance(res, dict) else None))
print('verdict:', ((res.get('conclusion') or {}) if isinstance(res, dict) else {}).get('verdict'))
PY

echo "[experiment] 完成。请打开 outputs/experiment_run_<run_id>_conclusion.md 查看结论。"
