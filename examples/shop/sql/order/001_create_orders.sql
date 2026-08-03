create table if not exists orders (
    id bigserial primary key,
    user_id bigint not null,
    total_cents bigint not null,
    created_at timestamptz not null default now()
);
