-- wecom-calendar-cli local store schema.
-- Three layers: raw facts (calendars/resources/events/attendees), sync audit
-- (sync_runs/sync_warnings/caldav_sync_state/metadata), and derived data
-- (event_instances). event_metadata is a separate, agent-owned layer that sync
-- never touches. Every statement uses CREATE ... IF NOT EXISTS and is applied on
-- each Open, so a fresh database is created and an existing one is left intact.
-- There is no column-level migration yet: additive columns on an already-created
-- database are not auto-applied. Introduce a versioned migration step here before
-- shipping a schema change that alters an existing table.

-- ============ meta ============
CREATE TABLE IF NOT EXISTS metadata (
    key   TEXT PRIMARY KEY,  -- 元信息键
    value TEXT NOT NULL      -- 元信息值
);

-- ============ 同步审计 ============
CREATE TABLE IF NOT EXISTS sync_runs (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    mode                TEXT NOT NULL,        -- 'incremental' | 'full'
    started_at          TEXT,                 -- ISO8601 UTC
    started_at_ms       INTEGER,
    finished_at         TEXT,
    finished_at_ms      INTEGER,
    calendars_scanned   INTEGER NOT NULL DEFAULT 0,
    resources_fetched   INTEGER NOT NULL DEFAULT 0,
    events_upserted     INTEGER NOT NULL DEFAULT 0,
    events_soft_deleted INTEGER NOT NULL DEFAULT 0,
    ok                  INTEGER NOT NULL DEFAULT 0,  -- 1 = 成功收尾
    error               TEXT                          -- 失败原因 (若有)
);

CREATE TABLE IF NOT EXISTS sync_warnings (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_run_id   INTEGER,
    category      TEXT NOT NULL,   -- 告警类别
    message       TEXT NOT NULL,   -- 告警内容
    created_at    TEXT,
    created_at_ms INTEGER
);
CREATE INDEX IF NOT EXISTS idx_sw_run ON sync_warnings(sync_run_id);

CREATE TABLE IF NOT EXISTS caldav_sync_state (
    calendar_id           TEXT PRIMARY KEY,  -- 日历本地 id
    ctag                  TEXT,              -- 上次成功同步时的 ctag
    sync_token            TEXT,              -- RFC6578 token (预留)
    last_full_scan_at     TEXT,
    last_full_scan_at_ms  INTEGER
);

-- ============ 原始事实 ============
CREATE TABLE IF NOT EXISTS calendars (
    calendar_id     TEXT PRIMARY KEY,  -- href 末段, 稳定本地 id
    href            TEXT NOT NULL,     -- 日历集合路径
    display_name    TEXT,              -- 展示名
    ctag            TEXT,              -- 最近一次见到的 ctag
    first_seen_at   TEXT,
    first_seen_at_ms INTEGER,
    last_seen_at    TEXT,
    last_seen_at_ms INTEGER,
    deleted_at      TEXT,              -- 软删除时刻 (服务端消失)
    deleted_at_ms   INTEGER
);

CREATE TABLE IF NOT EXISTS calendar_resources (
    calendar_id      TEXT NOT NULL,    -- 所属日历
    href             TEXT NOT NULL,    -- .ics 资源路径 (CalDAV 传输身份)
    etag             TEXT,             -- 资源 etag
    content_sha256   TEXT,             -- 正文哈希
    byte_size        INTEGER,          -- 正文字节数
    first_seen_at    TEXT,
    first_seen_at_ms INTEGER,
    last_seen_at     TEXT,
    last_seen_at_ms  INTEGER,
    last_changed_at  TEXT,             -- 正文变化时刻
    last_changed_at_ms INTEGER,
    deleted_at       TEXT,
    deleted_at_ms    INTEGER,
    last_sync_run_id INTEGER,
    PRIMARY KEY (calendar_id, href)
);
CREATE INDEX IF NOT EXISTS idx_cr_live ON calendar_resources(calendar_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_cr_etag ON calendar_resources(etag);

CREATE TABLE IF NOT EXISTS calendar_events (
    calendar_id       TEXT NOT NULL,   -- 所属日历
    uid               TEXT NOT NULL,   -- iCalendar UID
    recurrence_id_key TEXT NOT NULL,   -- master 为空串 '', 例外为规范化 key
    source_href       TEXT,            -- 来源资源 href
    summary           TEXT,
    description       TEXT,
    location          TEXT,
    dtstart_at        TEXT,
    dtstart_at_ms     INTEGER,
    dtend_at          TEXT,
    dtend_at_ms       INTEGER,
    all_day           INTEGER NOT NULL DEFAULT 0,
    status            TEXT,            -- CONFIRMED / CANCELLED / TENTATIVE
    sequence          INTEGER,
    rrule             TEXT,            -- 原始 RRULE 值
    recurrence_id_raw TEXT,
    organizer         TEXT,
    last_modified_at  TEXT,
    last_modified_at_ms INTEGER,
    raw_ics           TEXT NOT NULL,   -- 原始 iCalendar 留档 (可反复重建)
    first_seen_at     TEXT,
    first_seen_at_ms  INTEGER,
    last_seen_at      TEXT,
    last_seen_at_ms   INTEGER,
    deleted_at        TEXT,
    deleted_at_ms     INTEGER,
    last_sync_run_id  INTEGER,
    PRIMARY KEY (calendar_id, uid, recurrence_id_key)
);
CREATE INDEX IF NOT EXISTS idx_ce_start ON calendar_events(dtstart_at_ms);
CREATE INDEX IF NOT EXISTS idx_ce_live ON calendar_events(deleted_at);
CREATE INDEX IF NOT EXISTS idx_ce_uid ON calendar_events(uid);
CREATE INDEX IF NOT EXISTS idx_ce_href ON calendar_events(calendar_id, source_href);

CREATE TABLE IF NOT EXISTS event_attendees (
    calendar_id       TEXT NOT NULL,
    uid               TEXT NOT NULL,
    recurrence_id_key TEXT NOT NULL,
    email             TEXT NOT NULL,
    name              TEXT,
    response_status   TEXT,
    PRIMARY KEY (calendar_id, uid, recurrence_id_key, email)
);

-- ============ 派生 (sync 重建, 不含 Agent 数据) ============
CREATE TABLE IF NOT EXISTS event_instances (
    uid                 TEXT NOT NULL,   -- 逻辑事件 UID
    occurrence_key      TEXT NOT NULL,   -- 名义起始时刻规范化 key
    primary_calendar_id TEXT,            -- 主日历 (确定性选择)
    source_calendar_ids TEXT,            -- JSON 数组: 出现过的日历
    source_count        INTEGER NOT NULL DEFAULT 1,
    summary             TEXT,
    start_at            TEXT,
    start_at_ms         INTEGER,
    end_at              TEXT,
    end_at_ms           INTEGER,
    all_day             INTEGER NOT NULL DEFAULT 0,
    status              TEXT,
    local_date          TEXT,            -- 展示时区下的日期 YYYY-MM-DD
    PRIMARY KEY (uid, occurrence_key)
);
CREATE INDEX IF NOT EXISTS idx_ei_start ON event_instances(start_at_ms);
CREATE INDEX IF NOT EXISTS idx_ei_local_date ON event_instances(local_date);
CREATE INDEX IF NOT EXISTS idx_ei_uid ON event_instances(uid);

-- ============ Agent 维护层 (sync/expand 绝不触碰) ============
CREATE TABLE IF NOT EXISTS event_metadata (
    uid           TEXT NOT NULL,   -- 关联逻辑事件
    namespace     TEXT NOT NULL,   -- Agent 自定命名空间
    key           TEXT NOT NULL,   -- 命名空间内的键
    value_json    TEXT NOT NULL,   -- 任意 JSON 值
    source        TEXT NOT NULL DEFAULT 'agent',  -- 'agent' | 'user' | 'auto'
    created_at    TEXT,
    created_at_ms INTEGER,
    updated_at    TEXT,
    updated_at_ms INTEGER,
    PRIMARY KEY (uid, namespace, key)
);
CREATE INDEX IF NOT EXISTS idx_em_uid ON event_metadata(uid);
CREATE INDEX IF NOT EXISTS idx_em_ns ON event_metadata(namespace);
