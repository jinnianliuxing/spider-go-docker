# Modules - Go风格的模块组织

## 目录结构

```
modules/
├── user/              # 用户模块
│   ├── model.go       # 数据模型和DTO
│   ├── repository.go  # 数据访问层
│   ├── service.go     # 业务逻辑层
│   ├── handler.go     # HTTP处理层
│   └── module.go      # 模块初始化和依赖注入
├── grade/             # 成绩模块
├── course/            # 课程模块
├── exam/              # 考试模块
├── notice/            # 通知模块
└── admin/             # 管理员模块
```

## 设计原则

### 1. 按业务域组织 (Domain-Driven)
- 每个模块代表一个业务域（user、grade、course等）
- 模块内包含该业务的所有层（model、repository、service、handler）
- 模块之间通过接口通信，降低耦合

### 2. 依赖注入
- 每个模块的 `module.go` 是依赖注入的入口点
- 层级依赖：handler → service → repository
- 所有依赖通过构造函数注入，便于测试

### 3. 接口优先
- 每层都定义接口（Repository、Service）
- 实现与接口分离，便于 mock 和替换

### 4. Context传递
- 所有方法第一个参数都是 `context.Context`
- 用于超时控制、取消操作、传递请求上下文

## 文件职责

### model.go
- 数据模型定义
- 请求/响应 DTO
- 模型转换方法

```go
type User struct {
    ID        int       `json:"id"`
    Email     string    `json:"email"`
    CreatedAt time.Time `json:"created_at"`
}

type CreateUserRequest struct {
    Email string `json:"email" binding:"required,email"`
}
```

### repository.go
- 数据访问接口定义
- GORM数据库操作
- 只处理数据持久化，不包含业务逻辑

```go
type Repository interface {
    Create(ctx context.Context, user *User) error
    FindByID(ctx context.Context, id int) (*User, error)
}
```

### service.go
- 业务逻辑接口定义
- 核心业务逻辑实现
- 调用 repository、cache 等

```go
type Service interface {
    Create(ctx context.Context, req *CreateUserRequest) (*UserResponse, error)
    GetByID(ctx context.Context, id int) (*UserResponse, error)
}
```

### handler.go
- HTTP 请求处理
- 参数验证和绑定
- 调用 service 并返回响应
- 只处理 HTTP 层面的事情

```go
type Handler struct {
    service Service
}

func (h *Handler) Create(c *gin.Context) {
    var req CreateUserRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    // ...
}
```

### module.go
- 模块的依赖注入入口
- 初始化各层（repository → service → handler）
- 提供路由注册方法

```go
type Module struct {
    handler *Handler
}

func NewModule(db *gorm.DB) *Module {
    repo := NewRepository(db)
    service := NewService(repo)
    handler := NewHandler(service)
    return &Module{handler: handler}
}

func (m *Module) RegisterRoutes(r *gin.RouterGroup) {
    m.handler.RegisterRoutes(r)
}
```

## 使用示例

### 在主程序中初始化模块

```go
// main.go 或 container.go
func initModules(db *gorm.DB) *gin.Engine {
    r := gin.Default()
    api := r.Group("/api/v1")

    // 初始化用户模块
    userModule := user.NewModule(db)
    userModule.RegisterRoutes(api)

    // 初始化成绩模块
    gradeModule := grade.NewModule(db, cache)
    gradeModule.RegisterRoutes(api)

    return r
}
```

### 添加新模块

1. 创建模块目录：`mkdir internal/modules/yourmodule`
2. 创建文件：`model.go`, `repository.go`, `service.go`, `handler.go`, `module.go`
3. 在 `module.go` 中组装依赖
4. 在主程序中注册路由

### 跨模块调用

模块间通过 service 接口通信：

```go
// grade/service.go
type service struct {
    repo        Repository
    userService user.Service  // 依赖 user 模块
}

func NewService(repo Repository, userService user.Service) Service {
    return &service{
        repo:        repo,
        userService: userService,
    }
}

func (s *service) GetGrades(ctx context.Context, userID int) ([]Grade, error) {
    // 调用 user 模块验证用户
    _, err := s.userService.GetByID(ctx, userID)
    if err != nil {
        return nil, err
    }
    // ...
}
```

## 与老代码对比

### 老代码（Java风格）
```
internal/
├── controller/      # 所有控制器
├── service/         # 所有服务
├── repository/      # 所有仓储
├── dto/             # 所有DTO
└── model/           # 所有模型
```

**问题**：
- 功能分散在不同目录
- 难以看出模块边界
- 修改一个功能需要跨多个目录

### 新代码（Go风格）
```
internal/modules/
├── user/            # 用户模块（所有用户相关代码）
├── grade/           # 成绩模块（所有成绩相关代码）
└── course/          # 课程模块（所有课程相关代码）
```

**优点**：
- 高内聚：一个功能的所有代码在一起
- 低耦合：模块间通过接口通信
- 易维护：修改一个功能只需在一个目录内操作
- 易测试：每个模块可以独立测试

## 最佳实践

1. **保持模块独立**：模块不应直接依赖其他模块的内部实现
2. **使用接口通信**：跨模块调用通过 service 接口
3. **Context传递**：所有方法都接收 context.Context
4. **错误处理**：定义模块级别的错误变量（如 `ErrUserNotFound`）
5. **单一职责**：每个文件只负责一个层面的事情
6. **依赖注入**：通过构造函数注入依赖，避免全局变量

## 迁移策略

新功能使用新结构，老代码逐步重构：
1. 新功能直接在 `modules/` 下创建
2. 老代码可以逐步迁移（非必须）
3. 两种结构可以并存
