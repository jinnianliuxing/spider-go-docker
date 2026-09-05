# API 接口文档

Base URL: `/api`

## 统一响应格式

```json
{
  "code": 0,
  "message": "success",
  "data": {}
}
```

## 错误码

| 错误码 | 说明 |
|--------|------|
| 0 | 成功 |
| 40000 | 参数错误 |
| 40001 | 验证码错误 |
| 40002 | 教务系统参数错误 |
| 40003 | 教务系统未绑定 |
| 40004 | 教务系统登录失败 |
| 40005 | 教务系统解析失败 |
| 40006 | 教务系统请求失败 |
| 40007 | 该课程没有平时分数据 |
| 40008 | 绑定次数超限 |
| 40009 | 未完成教评 |
| 40010 | 教务系统登录超时 |
| 40011 | 需要多因素认证 |
| 40100 | 未授权 / 密码错误 |
| 40101 | Token 无效 |
| 40300 | 禁止访问 |
| 40400 | 用户不存在 |
| 40401 | 管理员不存在 |
| 40402 | 通知不存在 |
| 40404 | 资源不存在 |
| 40900 | 用户已存在 |
| 50000 | 内部错误 |
| 60001 | 微信登录失败 |
| 60002 | 微信绑定失败 |
| 60003 | 微信已被绑定 |

## 认证方式

需要认证的接口在请求头中携带 JWT Token：

```
Authorization: Bearer <token>
```

---

## 一、公开接口（无需认证）

### 1.1 用户注册

`POST /api/user/register`

**请求体：**
```json
{
  "name": "用户名",
  "email": "user@example.com",
  "password": "123456",
  "captcha": "123456"
}
```

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "token": "jwt_token",
    "message": "注册成功"
  }
}
```

### 1.2 用户登录

`POST /api/user/login`

**请求体：**
```json
{
  "email": "user@example.com",
  "password": "123456"
}
```

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "token": "jwt_token",
    "user": {
      "uid": 1,
      "email": "user@example.com",
      "name": "用户名",
      "sid": "2021001",
      "avatar": "",
      "created_at": "2024-01-01T00:00:00Z",
      "is_bind": true
    }
  }
}
```

### 1.3 重置密码

`POST /api/user/reset-password`

**请求体：**
```json
{
  "email": "user@example.com",
  "password": "new_password",
  "captcha": "123456"
}
```

### 1.4 微信登录/注册

`POST /api/user/wechat/login`

**请求体：**
```json
{
  "code": "wx_auth_code"
}
```

**响应：** 同用户登录

### 1.5 发送邮箱验证码

`POST /api/captcha/send`

**请求体：**
```json
{
  "email": "user@example.com"
}
```

### 1.6 获取当前学期

`GET /api/config/term`

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "term": "2024-2025-1"
  }
}
```

### 1.7 获取学期日期

`GET /api/config/semester-dates`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| term | string | 否 | 学期，如 `2024-2025-1`，不传返回当前学期 |

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "term": "2024-2025-1",
    "start_date": "2024-09-01",
    "end_date": "2025-01-15"
  }
}
```

### 1.8 获取通知列表

`GET /api/notices`

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": [...]
}
```

### 1.9 获取通知详情

`GET /api/notices/:id`

### 1.10 获取使用须知

`GET /api/introductions`

### 1.11 查看分享的课程表

`GET /api/share/course/:code`

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "user_name": "分享者昵称",
    "term": "2024-2025-1",
    "start_week": 1,
    "end_week": 5,
    "schedule": {}
  }
}
```

---

## 二、用户接口（需要 JWT 认证）

### 2.1 获取用户信息

`GET /api/user/info`

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "uid": 1,
    "email": "user@example.com",
    "name": "用户名",
    "sid": "2021001",
    "avatar": "",
    "created_at": "2024-01-01T00:00:00Z",
    "is_bind": true
  }
}
```

### 2.2 绑定教务系统

`POST /api/user/bind`

**请求体：**
```json
{
  "sid": "2021001",
  "spwd": "教务系统密码"
}
```

### 2.3 检查绑定状态

`GET /api/user/is-bind`

**响应：**
```json
{ "is_bind": true }
```

### 2.4 获取绑定状态详情

`GET /api/user/bind-status`

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "is_bound": true,
    "current_sid": "2021001",
    "total_bind_count": 2,
    "last_bind_at": "2024-01-01T00:00:00Z",
    "can_change_sid": false
  }
}
```

### 2.5 绑定微信

`POST /api/user/wechat/bind`

**请求体：**
```json
{
  "code": "wx_auth_code"
}
```

### 2.6 更新用户名

`POST /api/user/update-name`

**请求体：**
```json
{
  "name": "新用户名"
}
```

### 2.7 更新邮箱

`POST /api/user/update-email`

**请求体：**
```json
{
  "email": "new@example.com",
  "captcha": "123456"
}
```

---

## 三、成绩模块（需要 JWT 认证）

### 3.1 获取成绩

`GET /api/user/grades`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| term | string | 否 | 学期，如 `2024-2025-1` |
| year | string | 否 | 学年，如 `2024-2025`（优先级高于 term） |

不传参数返回所有成绩。

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "grades": [
      {
        "serial_no": "001",
        "term": "2024-2025-1",
        "code": "CS101",
        "subject": "高等数学",
        "score": "92",
        "credit": 4.0,
        "gpa": 4.2,
        "status": 0,
        "property": "必修",
        "flag": ""
      }
    ],
    "gpa": {
      "average_gpa": 3.85,
      "average_score": 88.5,
      "basic_score": 85.2
    }
  }
}
```

### 3.2 获取等级考试成绩

`GET /api/user/grades/level`

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "no": "001",
      "CourseName": "大学英语四级",
      "LevelGrade": "520",
      "Time": "2024-06"
    }
  ]
}
```

### 3.3 获取成绩分析

`GET /api/user/grades/analysis`

返回最近三个学期的成绩分析数据。

### 3.4 获取平时分

`POST /api/user/grades/regular`

**请求体：**
```json
{
  "term": "2024-2025-1",
  "code": "CS101"
}
```

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "final_exam_score": "85",
    "final_exam_ratio": "70%",
    "regular_score": "90",
    "regular_ratio": "30%",
    "final_score": "87"
  }
}
```

### 3.5 获取学生信息

`GET /api/user/grades/student-info`

返回年级、学院、专业、班级等信息。

---

## 四、课程模块（需要 JWT 认证）

### 4.1 获取课程表

`GET /api/user/courses`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| week | int | 是 | 周次（1-20） |
| term | string | 是 | 学期，如 `2024-2025-1` |

---

## 五、考试模块（需要 JWT 认证）

### 5.1 获取考试安排

`GET /api/user/exams`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| term | string | 是 | 学期，如 `2024-2025-1` |

---

## 六、教评模块（需要 JWT 认证）

### 6.1 获取教评任务列表

`GET /api/user/evaluation/tasks`

### 6.2 获取待评课程列表

`GET /api/user/evaluation/courses`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| taskid | int | 是 | 任务 ID |

### 6.3 获取评教题目

`GET /api/user/evaluation/questions`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| indexid | string | 是 | 索引 ID |
| pjcoursetype | string | 是 | 评教课程类型 |

### 6.4 提交评教

`POST /api/user/evaluation/submit`

**请求体：** 评教数据数组

### 6.5 自动评教

`POST /api/user/evaluation/auto`

### 6.6 查看评教状态

`GET /api/user/evaluation/status`

---

## 七、排名模块（需要 JWT 认证）

### 7.1 获取我的排名

`GET /api/user/ranking/my`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| statistics_type | string | 否 | `cumulative`（默认）/ `semester` / `year` |
| statistics_term | string | 否 | 学期/学年，如 `2024-2025-1` 或 `2024-2025` |

---

## 八、数据同步模块（需要 JWT 认证）

### 8.1 触发同步任务

`POST /api/user/sync/trigger`

**请求体：**
```json
{
  "task_type": "all",
  "user_ids": []
}
```

`task_type` 可选值：`all` / `grade` / `regular_grade` / `exam` / `level_exam` / `course`

`user_ids` 可选，为空则同步当前用户。

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "task_id": "uuid",
    "task_type": "all",
    "trigger_type": "manual",
    "status": 0,
    "total_users": 1,
    "created_at": "2024-01-01T00:00:00Z"
  }
}
```

### 8.2 获取同步任务列表

`GET /api/user/sync/tasks`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| limit | int | 否 | 每页数量，默认 20 |
| offset | int | 否 | 偏移量，默认 0 |

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tasks": [...],
    "total": 100,
    "limit": 20,
    "offset": 0
  }
}
```

### 8.3 获取同步任务详情

`GET /api/user/sync/tasks/:taskId`

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "task": {
      "task_id": "uuid",
      "task_type": "all",
      "status": 2,
      "total_users": 1,
      "processed_users": 1,
      "success_users": 1,
      "new_records": 10,
      "updated_records": 2,
      "deleted_records": 0,
      "unchanged_records": 50,
      "start_time": "...",
      "end_time": "..."
    },
    "logs": [...]
  }
}
```

任务状态：`0` 待执行 / `1` 执行中 / `2` 成功 / `3` 失败

### 8.4 获取用户同步状态

`GET /api/user/sync/status`

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "uid": 1,
    "grade_status": {
      "last_sync_at": "2024-01-01T00:00:00Z",
      "last_task_id": "uuid",
      "sync_version": 1704067200,
      "record_count": 50
    },
    "regular_grade_status": { ... },
    "exam_status": { ... },
    "level_exam_status": { ... },
    "course_status": { ... }
  }
}
```

---

## 九、分享模块（需要 JWT 认证）

### 9.1 创建课程表分享

`POST /api/user/share/course`

**请求体：**
```json
{
  "term": "2024-2025-1",
  "week": 5
}
```

或范围分享：
```json
{
  "term": "2024-2025-1",
  "start_week": 1,
  "end_week": 5
}
```

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "token": "share_token_string"
  }
}
```

---

## 十、选课提示模块（需要 JWT 认证）

### 10.1 获取体育选修课教师统计

`GET /api/user/course-tips`

**参数：**
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| course_name | string | 是 | 课程名称：`体育选项课Ⅰ` / `体育选项课Ⅱ` / `体育选项课Ⅲ` |

**说明：**
- 数据来源：通过 grades 表与 courses 表按 uid + 课程名称关联，获取教师-成绩对应关系
- 过滤规则：学生人数少于 30 的教师会被自动排除（排除数据异常导致的误关联）
- 缓存策略：结果缓存 24 小时

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "course_name": "体育选项课Ⅰ",
    "teachers": [
      {
        "teacher_name": "张三",
        "student_count": 120,
        "average_score": 82.5,
        "max_score": 98,
        "min_score": 45,
        "fail_rate": 0.025,
        "score_distribution": {
          "range_0_59": 3,
          "range_60_69": 10,
          "range_70_79": 30,
          "range_80_89": 50,
          "range_90_100": 27
        }
      }
    ]
  }
}
```

---

## 十一、管理员接口（需要管理员 Token）

### 11.1 管理员登录

`POST /api/admin/login`（公开）

**请求体：**
```json
{
  "email": "admin@example.com",
  "password": "admin_password"
}
```

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "token": "admin_jwt_token",
    "admin": {
      "uid": 1,
      "email": "admin@example.com",
      "name": "管理员",
      "avatar": "",
      "created_at": "2024-01-01T00:00:00Z"
    }
  }
}
```

### 11.2 获取管理员信息

`GET /api/admin/info`

### 11.3 修改管理员密码

`POST /api/admin/reset`

**请求体：**
```json
{
  "old_password": "old_pwd",
  "new_password": "new_pwd"
}
```

### 11.4 群发邮件

`POST /api/admin/broadcast-email`

**请求体：**
```json
{
  "subject": "邮件标题",
  "content": "邮件内容"
}
```

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "success_count": 100,
    "fail_count": 2,
    "total_count": 102
  }
}
```

### 11.5 管理员同步所有用户

`POST /api/admin/sync/all`

**请求体：**
```json
{
  "task_type": "all"
}
```

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "task": { ... },
    "total_users": 500,
    "message": "同步任务已启动"
  }
}
```

### 11.6 获取同步任务列表（管理员）

`GET /api/admin/sync/tasks`

参数同 8.2

### 11.7 获取同步任务详情（管理员）

`GET /api/admin/sync/tasks/:taskId`

### 11.8 优化同步表

`POST /api/admin/sync/optimize`

执行 `OPTIMIZE TABLE`，释放 DELETE 后的磁盘空间。建议在低峰期执行，会锁表。

**响应：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "message": "OPTIMIZE TABLE 执行成功"
  }
}
```

### 11.9 设置当前学期

`POST /api/admin/config/term`

**请求体：**
```json
{
  "term": "2024-2025-2"
}
```

### 11.10 设置学期日期

`POST /api/admin/config/semester-dates`

**请求体：**
```json
{
  "term": "2024-2025-1",
  "start_date": "2024-09-01",
  "end_date": "2025-01-15"
}
```

### 11.11 通知管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/notices` | 获取所有通知 |
| POST | `/api/admin/notices` | 创建通知 |
| PUT | `/api/admin/notices/:id` | 更新通知 |
| DELETE | `/api/admin/notices/:id` | 删除通知 |

### 11.12 使用须知管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/introductions` | 获取所有使用须知 |
| POST | `/api/admin/introductions` | 创建使用须知 |
| PUT | `/api/admin/introductions/:id` | 更新使用须知 |
| DELETE | `/api/admin/introductions/:id` | 删除使用须知 |

### 11.13 统计接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/statistics/dau` | 获取今日 DAU |
| GET | `/api/admin/statistics/dau/range` | 获取日期范围 DAU（参数：`start_date`, `end_date`） |
| GET | `/api/admin/statistics/user/count` | 获取用户总数 |
| GET | `/api/admin/statistics/user/new` | 获取新增用户数（参数：`date` 或 `start_date`+`end_date`） |
