create table if not exists users (
    id bigserial primary key,
    created_at timestamptz not null default now()
);
