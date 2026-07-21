-- scheduler_endpoints —— 调度端点配置（权重/并发/启用，可动态调整）。
--
-- 一行 = 某个模型能力(capability) 下的一个「平台账号」端点。
-- 端点的实际调用函数(handler)在各平台包代码里按 (platform, account, capability) 注册；
-- 本表只存可动态调整的数据。二者按三元组关联——严格模式下，DB 无对应行的 handler 不上线。
--
-- 动态调权重示例：
--   UPDATE public.scheduler_endpoints SET weight = 8
--   WHERE platform='chatgpt' AND account='azure-2' AND capability='gpt-image-2';
-- 改动在调度器缓存过期后（≤60s）生效。

CREATE TABLE IF NOT EXISTS public.scheduler_endpoints (
    id           BIGSERIAL PRIMARY KEY,
    platform     TEXT        NOT NULL,
    account      TEXT        NOT NULL,
    capability   TEXT        NOT NULL,
    weight       INT         NOT NULL DEFAULT 1,
    max_inflight INT         NOT NULL DEFAULT 0,
    enabled      BOOLEAN     NOT NULL DEFAULT TRUE,
    extra_params JSONB       NOT NULL DEFAULT '{}',
    inflight_scope smallint  NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_scheduler_endpoint UNIQUE (platform, account, capability)
    );

CREATE INDEX IF NOT EXISTS idx_scheduler_endpoint_cap
    ON public.scheduler_endpoints (capability);
