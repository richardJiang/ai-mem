# 外挂记忆实验系统

最小可验证架构（MVP）用于论证外挂记忆的5个核心特性。

## 架构目标

论证以下三点：

1. **外挂记忆有哪些本质特性**
   - 可持久
   - 可演化（不是只存日志）
   - 可被抽象（从具体经验到规则）
   - 可被检索并影响未来行为

2. **外挂记忆是否能显著减少重复劳动**
   - 相同/相似任务下，随着次数增加，所需推理步数、token、错误率下降

3. **AI是否可以被"教育"**
   - 教练给反馈（对/错/更优解）
   - AI能自我反思
   - 把反馈转化为可复用的抽象知识
   - 后续不再犯同类错误

## 技术栈

- **后端**: Go 1.21 + Gin + GORM + MySQL
- **AI服务**: Dify API
- **前端**: HTML + JavaScript (原生)
- **数据库**: MySQL + Redis（可选）
- **外部长期记忆（可选）**: MemOS（用于跨 run 的长期记忆服务）

## 项目结构

```
mem-test/
├── config/              # 配置文件
├── internal/
│   ├── config/         # 配置加载
│   ├── db/             # 数据库初始化
│   ├── model/          # 数据模型
│   ├── service/        # 业务逻辑
│   │   ├── agent.go           # Agent服务（调用Dify）
│   │   ├── coach.go           # Coach服务（反馈）
│   │   ├── reflection.go      # Reflection服务（反思）
│   │   ├── dify_client.go     # Dify客户端
│   │   └── service_context.go # 服务上下文
│   ├── handler/        # HTTP处理器
│   │   ├── task_handler.go      # 任务相关
│   │   ├── memory_handler.go    # 记忆相关
│   │   └── experiment_handler.go # 实验统计
│   └── router/         # 路由配置
├── frontend/           # 前端页面
└── main.go            # 入口文件
```

## 核心模块

### 1. Agent（执行任务）
- 接收任务输入
- 检索相关记忆
- 构建提示词（注入记忆）
- 调用Dify执行任务
- 记录任务结果

### 2. Coach（反馈）
- 人工反馈
- 规则引擎自动判断
- 生成反馈记录

### 3. Reflection（反思）
- 接收反馈
- 调用Dify进行反思
- 提取抽象规则
- 保存到记忆库

### 4. Memory（记忆）
- 存储抽象规则
- 支持版本演化
- 关键词检索
- 使用统计

## 数据库设计

### memories（记忆表）
- `trigger`: 触发条件
- `lesson`: 学到的经验
- `confidence`: 置信度
- `version`: 版本号（支持演化）
- `use_count`: 使用次数

### tasks（任务表）
- `task_type`: 任务类型
- `input`: 输入
- `output`: 输出
- `is_correct`: 是否正确
- `memory_ids`: 使用的记忆ID
- `token_count`: Token消耗
- `group_type`: 实验组（A-F）

### feedbacks（反馈表）
- `task_id`: 关联任务
- `type`: 反馈类型
- `content`: 反馈内容
- `used_for_memory`: 是否已用于生成记忆

## 实验设计

### 实验任务（单参数 / 多参数）

本项目内置了多种任务类型，用于验证“记忆能否复用经验、能否适应规则变化、能否泛化”。

- **`task_type=lottery`（单参数）**
  - 输入核心字段：`points`（积分）
  - 目标：判断是否允许抽奖并输出严格 JSON：`{"allow": true, "reason": "..."}`
  - 适合验证：规则门槛变化时，记忆是否会被旧规则带偏、是否能快速纠错

- **`task_type=lottery_multi`（多参数）**
  - 输入包含多个“积分相关”字段（并非所有字段都可计入，且规则未显式给出）
  - 适合验证：在信息更复杂、规则更隐含时，记忆检索/门控/自检是否能提升稳定性

- **`task_type=lottery_v2`（多约束）**
  - 额外约束：黑名单、VIP门槛、每日次数上限等
  - 适合验证：多条件组合下的错误归因与规则抽象能力

### 规则变化模式（Rule Mode）

实验支持在同一个 run 内“规则随轮次变化”，模拟线上策略调整带来的分布漂移：

- **`rule_mode=none`**：规则不变（更偏向验证“记忆是否能减少重复错误”）
- **`rule_mode=low`**：低频变更（规则偶尔变化）
- **`rule_mode=high`**：高频变更（规则经常变化，最考验“变更检测/快速切换”）

> 注意：组内不会读取真实阈值做“作弊”，只依赖 judge 的 correct/incorrect 反馈与输入本身。

### 六组对照（A-F）

所有实验组都在**相同输入序列**上对比；差异仅来自“是否/如何使用记忆、如何纠错、如何固化”。

- **A 组（No Memory）**
  - 不检索/不写入记忆
  - 作为下限对照

- **B 组（Log Memory）**
  - 检索少量历史“正确案例日志”（不抽象规则）
  - 目标：验证“只存日志”对泛化的局限

- **C 组（Run-local Reflection Memory）**
  - 仅在当前 run 内：判错 → 反思 → 抽象规则写入（run 内隔离）
  - 目标：验证“反思→规则”相对 B 的优势（test-time learning）

- **D 组（STM + Global MTM + MemOS）**
  - **短期记忆（STM）**：注入近期判错反馈，优先避免重复犯错
  - **中期记忆（MTM）**：在 run 内写入反思规则，同时把经验**固化到全局中期记忆池**（`run_id=0` 且 `derived_from=global|...`），实现跨 run 复用
  - **长期记忆（LTM，可选）**：允许接入 MemOS 作为外部候选记忆
  - 目标：验证“跨 run 复用经验”的收益

- **E 组（Two-stage Self-check + Rerank + Validated Consolidation）**
  - **两阶段推理**：先生成初稿，再基于“已验证规则 + 近期错误信号 + 外部候选记忆”进行自检纠错
  - **输入感知重排**：对候选记忆按与输入的相关性重排，减少无关规则污染
  - **验证固化**：新规则进入全局池前做快速一致性验证（降低噪声固化）
  - 目标：在 D 的基础上提升稳定性、降低错误传播

- **F 组（Change Detection + Epoch + Bandit Competition + Self-check）**
  - **变更检测**：连续判错触发进入新 epoch（不读取真实规则阈值，不作弊）
  - **候选竞争（Bandit/UCB）**：从多条候选规则中选择“主规则”注入；判错则短期封禁该规则，避免下一轮继续被带偏
  - **探索窗口**：变更后短期探索，减少“top1 锁死”导致的适应迟缓
  - **自检纠错**：复用 E 的二阶段审校输出
  - 目标：在 `rule_mode=high` 这种高频变化场景显著优于 E（更快切换、更少负迁移）

### 对比指标

- 错误次数随任务次数变化
- 平均token数
- 是否重复犯同类错误
- 是否能举一反三

此外，实验运行会输出：

- **错误率 Wilson 95% 置信区间（CI95）**
- **两比例 z-test（p-value）**：例如 `F_vs_E`, `E_vs_D` 等
- **趋势曲线**：累计错误/累计正确率、首次出错轮次等（前端可视化）

## 快速开始

### 1. 配置数据库

创建MySQL数据库：

```sql
CREATE DATABASE mem_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

修改 `config/config.yaml` 中的数据库配置。

### 2. 安装依赖

```bash
go mod tidy
```

### 3. 运行服务

```bash
make run
```

服务将在 `http://localhost:8080` 启动。

### 4. 打开前端

在浏览器中打开 `frontend/index.html`，或使用HTTP服务器：

```bash
cd frontend
python3 -m http.server 3000
```

然后访问 `http://localhost:3000`

## API接口

### 任务相关

- `POST /api/tasks/execute` - 执行任务
- `POST /api/tasks/feedback` - 提交反馈
- `POST /api/tasks/judge` - 自动判断
- `POST /api/tasks/reflect` - 反思并保存

### 记忆相关

- `GET /api/memories` - 列出所有记忆
- `GET /api/memories/:id` - 获取单个记忆
- `DELETE /api/memories/:id` - 删除记忆

### 实验相关

- `GET /api/experiments/stats?group_type=A` - 获取统计
- `GET /api/experiments/compare` - 对比 A-F 组（全局数据视角）
- `GET /api/experiments/trend?mode=low|high|none&task_type=lottery|lottery_multi&run_id=...` - 获取某次 run 的曲线
- `GET /api/experiments/compare-modes?task_type=...` - 对比 low/high 两种模式下的组间表现
- `POST /api/experiments/run` - 跑一次实验（推荐走脚本/Makefile）
- `POST /api/experiments/reset` - 清空实验数据

## 使用示例

### 1. 执行任务

```bash
curl -X POST http://localhost:8080/api/tasks/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_type": "lottery",
    "input": "{\"points\": 50, \"action\": \"lottery\"}",
    "group_type": "C",
    "use_memory": true
  }'
```

### 2. 提交反馈

```bash
curl -X POST http://localhost:8080/api/tasks/feedback \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": 1,
    "feedback_type": "incorrect",
    "content": "积分不足时应该拒绝抽奖"
  }'
```

### 3. 查看记忆

```bash
curl http://localhost:8080/api/memories
```

## 如何跑实验（推荐）

### 1) Makefile 一键跑（默认 A-F 全组）

你可以通过环境变量控制参数：

- `EXP_HOST`：后端地址（默认 `http://localhost:8080`）
- `EXP_RUNS`：每组轮次数（默认 `100`，建议至少 `>=30`）
- `EXP_GROUPS`：默认 `["A","B","C","D","E","F"]`（一般不需要改）

常用命令：

```bash
# 单参数（lottery）规则不变/低频/高频
make experiment
make experiment-low
make experiment-high

# 多参数（lottery_multi）规则不变/低频/高频
make experiment-multi
make experiment-multi-low
make experiment-multi-high
```

### 2) 脚本参数（更细粒度）

底层由 `scripts/run_experiment_100.sh` 发送请求到 `/api/experiments/run`，核心参数：

- `TASK_TYPE=lottery | lottery_multi | lottery_v2`
- `RUNS=100`（每组轮次数）
- `RULE_MODE=none | low | high`
- `SEED=0`（0 表示用当前时间）
- `EXP_GROUPS='["A","B","C","D","E","F"]'`

示例：

```bash
HOST=http://localhost:8080 RUNS=100 TASK_TYPE=lottery_multi RULE_MODE=high ./scripts/run_experiment_100.sh
```

### 3) 输出与复盘位置

每次 `/api/experiments/run` 会写入：

- `outputs/experiment_run_<run_id>.json`
- `outputs/experiment_run_<run_id>_conclusion.md`

前端页面 `frontend/index.html` 可直接加载最近一次的曲线/对比（或指定 run_id）。

## 推荐测试方案（论文级可复现）

为了避免偶然性，建议用“多模式 × 多任务 × 多 seed”的矩阵：

1. **任务维度**
   - 单参数：`task_type=lottery`
   - 多参数：`task_type=lottery_multi`

2. **规则变更维度**
   - `rule_mode=none`（看是否减少重复错误）
   - `rule_mode=low`（看是否稳定迁移）
   - `rule_mode=high`（看是否快速适应变更；重点检验 F）

3. **样本量与随机性**
   - 每组至少 `EXP_RUNS>=30`，更建议 `50/100`
   - 每种设置跑 `>=3` 个不同 `SEED`（或多次运行），看结论一致性

4. **判定与结论**
   - 先看 `error_rate` 与 `CI95`（是否区间明显分离）
   - 再看 `p_value`（例如 `F_vs_E` 是否 < 0.05）
   - 最后看趋势曲线：高频变更下，F 是否能更快从错误中恢复（首次出错轮次、累计错误曲线斜率）

## 验证5个特性

### 1. 持久性
- 重启系统后，记忆仍然存在
- 相同任务可以复用经验

### 2. 可抽象性
- 记忆不是对话日志，而是规则
- 从"发生了什么"到"学到了什么"

### 3. 可演化性
- 同一规则可以被修正、覆盖、增强
- 版本号递增

### 4. 可检索并影响决策
- 记忆被检索并注入到提示词
- 明确影响AI的回答

### 5. 可教育性
- 通过外部反馈塑造记忆
- 错误反馈触发反思
- 反思结果转化为可复用规则

## 注意事项

1. 确保MySQL服务运行
2. 确保Dify API可访问
3. 首次运行会自动创建表结构
4. 前端需要配置正确的API地址

## 开发规范

遵循Go微服务开发规范：
- 文件名使用下划线分隔（小写）
- 变量/函数使用驼峰命名
- 错误处理使用标准错误码
- 日志使用context追踪

