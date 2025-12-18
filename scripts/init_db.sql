-- 创建数据库
CREATE DATABASE IF NOT EXISTS mem_test CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE mem_test;

-- 注意：表结构会由GORM自动创建，这里只是参考
-- 如果需要手动创建，可以执行以下SQL

-- CREATE TABLE IF NOT EXISTS memories (
--     id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
--     created_at DATETIME(3) NULL,
--     updated_at DATETIME(3) NULL,
--     deleted_at DATETIME(3) NULL,
--     trigger VARCHAR(500) NOT NULL,
--     lesson TEXT NOT NULL,
--     derived_from VARCHAR(200),
--     apply_to VARCHAR(200),
--     confidence DECIMAL(3,2) DEFAULT 0.5,
--     version INT DEFAULT 1,
--     use_count INT DEFAULT 0,
--     INDEX idx_trigger (trigger),
--     INDEX idx_deleted_at (deleted_at)
-- );

-- CREATE TABLE IF NOT EXISTS tasks (
--     id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
--     created_at DATETIME(3) NULL,
--     updated_at DATETIME(3) NULL,
--     deleted_at DATETIME(3) NULL,
--     task_type VARCHAR(100) NOT NULL,
--     input TEXT NOT NULL,
--     output TEXT,
--     is_correct BOOLEAN,
--     memory_ids VARCHAR(500),
--     token_count INT,
--     group_type VARCHAR(10),
--     INDEX idx_task_type (task_type),
--     INDEX idx_group_type (group_type),
--     INDEX idx_deleted_at (deleted_at)
-- );

-- CREATE TABLE IF NOT EXISTS feedbacks (
--     id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
--     created_at DATETIME(3) NULL,
--     updated_at DATETIME(3) NULL,
--     deleted_at DATETIME(3) NULL,
--     task_id BIGINT UNSIGNED NOT NULL,
--     type VARCHAR(20) NOT NULL,
--     content TEXT NOT NULL,
--     used_for_memory BOOLEAN DEFAULT FALSE,
--     memory_id BIGINT UNSIGNED,
--     INDEX idx_task_id (task_id),
--     INDEX idx_deleted_at (deleted_at)
-- );

