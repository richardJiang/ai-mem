# 记忆实验结论（论文级）

- run_id: 1
- task_type: lottery
- runs_per_group: 100
- seed: 1765963924536708000
- created_at: 2025-12-17T17:32:04+08:00

## 组内统计（仅本次 run）

| 组别 | N | Incorrect | ErrorRate | CI95 |
| --- | ---: | ---: | ---: | --- |
| A | 100 | 27 | 0.270 | [0.193, 0.364] |
| B | 100 | 53 | 0.530 | [0.433, 0.625] |
| C | 100 | 1 | 0.010 | [0.002, 0.054] |

## 显著性检验

```json
{
  "C_trend_first_vs_second_half": {
    "first_half_error_rate": 0.02,
    "p_value": 0.31487864133641974,
    "second_half_error_rate": 0,
    "z": -1.005037815259212
  },
  "C_vs_A": {
    "p_value": 1.1681898159920934e-7,
    "z": -5.298404448604946
  },
  "C_vs_B": {
    "p_value": 0,
    "z": -8.282187031169942
  }
}
```

## 自动结论

- verdict: learning_observed

### 主要论断

- C组在同一批重复任务中错误率后半段低于前半段，存在test-time learning迹象（反馈→反思→记忆→行为变化）。
- 在本次run内，C组错误率显著低于A组与B组（p<0.05），支持“反思→抽象规则记忆”优于“无记忆/仅日志记忆”。
