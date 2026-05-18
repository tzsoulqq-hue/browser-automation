create table if not exists browser_automation_sessions (
  session_id text primary key,
  request_id text,
  status integer not null,
  labels jsonb not null default '{}'::jsonb,
  data jsonb not null,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  expires_at timestamptz
);

create unique index if not exists browser_automation_sessions_request_id_idx
  on browser_automation_sessions(request_id)
  where request_id is not null;

create index if not exists browser_automation_sessions_status_updated_idx
  on browser_automation_sessions(status, updated_at desc, session_id asc);

create index if not exists browser_automation_sessions_labels_idx
  on browser_automation_sessions using gin(labels);

create table if not exists browser_automation_tasks (
  task_id text primary key,
  request_id text,
  session_id text not null,
  status integer not null,
  task_key text not null,
  scenario_key text not null default '',
  labels jsonb not null default '{}'::jsonb,
  data jsonb not null,
  created_at timestamptz not null,
  updated_at timestamptz not null,
  completed_at timestamptz
);

create unique index if not exists browser_automation_tasks_request_id_idx
  on browser_automation_tasks(request_id)
  where request_id is not null;

create index if not exists browser_automation_tasks_session_status_idx
  on browser_automation_tasks(session_id, status, updated_at desc, task_id asc);

create index if not exists browser_automation_tasks_key_idx
  on browser_automation_tasks(task_key, scenario_key, created_at desc, task_id asc);

create index if not exists browser_automation_tasks_labels_idx
  on browser_automation_tasks using gin(labels);
