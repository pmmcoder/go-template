-- AI 任务表
CREATE TABLE IF NOT EXISTS ai_tasks (
    id            BIGSERIAL PRIMARY KEY,
    user_id       VARCHAR(100) NOT NULL DEFAULT '',
    model         VARCHAR(100) NOT NULL DEFAULT '',
    model_opts    JSONB        NOT NULL DEFAULT '{}',
    messages      JSONB        NOT NULL DEFAULT '{}',
    status        SMALLINT     NOT NULL DEFAULT 1,
    content       TEXT         NOT NULL DEFAULT '',
    error_message TEXT         NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_tasks_status     ON ai_tasks (status);
CREATE INDEX IF NOT EXISTS idx_ai_tasks_created_at ON ai_tasks (created_at DESC);
