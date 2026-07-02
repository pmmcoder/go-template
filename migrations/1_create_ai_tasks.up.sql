-- AI 任务表
CREATE TABLE IF NOT EXISTS ai_tasks (
    id            BIGSERIAL PRIMARY KEY,
    model         TEXT        NOT NULL,
    messages      JSONB       NOT NULL,
    status        SMALLINT    NOT NULL,         -- 1=pending 2=running 3=succeeded 4=failed
    content       TEXT        NOT NULL DEFAULT '',
    total_tokens  INTEGER     NOT NULL DEFAULT 0,
    error_message TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_tasks_status     ON ai_tasks (status);
CREATE INDEX IF NOT EXISTS idx_ai_tasks_created_at ON ai_tasks (created_at DESC);
