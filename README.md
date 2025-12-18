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
- `group_type`: 实验组（A/B/C）

### feedbacks（反馈表）
- `task_id`: 关联任务
- `type`: 反馈类型
- `content`: 反馈内容
- `used_for_memory`: 是否已用于生成记忆

## 实验设计

### 三组对照

- **A组**: 无外挂记忆
- **B组**: 有外挂记忆（无反思，仅存对话）
- **C组**: 有外挂记忆 + 反思 + 抽象规则

### 对比指标

- 错误次数随任务次数变化
- 平均token数
- 是否重复犯同类错误
- 是否能举一反三

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
go run main.go
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
- `GET /api/experiments/compare` - 对比A/B/C组
- `GET /api/experiments/trend?group_type=C` - 获取错误趋势

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

