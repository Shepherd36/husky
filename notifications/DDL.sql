-- SPDX-License-Identifier: ice License 1.0
--************************************************************************************************************************************
-- users
CREATE TABLE IF NOT EXISTS users  (
                    last_ping_cooldown_ended_at             TIMESTAMP,
                    disabled_push_notification_domains      TEXT[],
                    disabled_email_notification_domains     TEXT[],
                    disabled_sms_notification_domains       TEXT[],
                    agenda_contact_user_ids                 TEXT[],
                    phone_number                            TEXT,
                    email                                   TEXT,
                    first_name                              TEXT,
                    last_name                               TEXT,
                    user_id                                 TEXT NOT NULL primary key,
                    username                                TEXT,
                    profile_picture_name                    TEXT,
                    referred_by                             TEXT,
                    phone_number_hash                       TEXT,
                    telegram_user_id                        TEXT,
                    telegram_bot_id                         TEXT,
                    language                                TEXT NOT NULL default 'en'
                  );
ALTER TABLE users ADD COLUMN IF NOT EXISTS telegram_user_id text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS telegram_bot_id text;
--************************************************************************************************************************************
-- sent_notifications
CREATE TABLE IF NOT EXISTS sent_notifications  (
                    sent_at                     TIMESTAMP NOT NULL,
                    language                    TEXT NOT NULL,
                    user_id                     TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
                    uniqueness                  TEXT NOT NULL,
                    notification_type           TEXT NOT NULL,
                    notification_channel        TEXT NOT NULL,
                    notification_channel_value  TEXT NOT NULL,
                    primary key(user_id,uniqueness,notification_type,notification_channel,notification_channel_value));
CREATE INDEX IF NOT EXISTS sent_notifications_sent_at_ix ON sent_notifications (sent_at);
--************************************************************************************************************************************
-- scheduled_notifications
CREATE TABLE IF NOT EXISTS scheduled_notifications  (
                    i                           BIGINT generated always as identity NOT NULL,
                    scheduled_at                TIMESTAMP NOT NULL,
                    scheduled_for               TIMESTAMP NOT NULL,
                    data                        JSONB NOT NULL,
                    language                    TEXT NOT NULL,
                    user_id                     TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
                    uniqueness                  TEXT NOT NULL,
                    notification_type           TEXT NOT NULL,
                    notification_channel        TEXT NOT NULL,
                    notification_channel_value  TEXT NOT NULL,
                    primary key(user_id,uniqueness,notification_type,notification_channel,notification_channel_value));
CREATE UNIQUE INDEX IF NOT EXISTS scheduled_notifications_i_ix ON scheduled_notifications (i);
CREATE INDEX IF NOT EXISTS scheduled_notifications_user_id_notification_type_ix ON scheduled_notifications (user_id,notification_type);
--************************************************************************************************************************************
-- sent_announcements
CREATE TABLE IF NOT EXISTS sent_announcements (
                    sent_at                         TIMESTAMP NOT NULL,
                    language                        TEXT NOT NULL,
                    uniqueness                      TEXT NOT NULL,
                    notification_type               TEXT NOT NULL,
                    notification_channel            TEXT NOT NULL,
                    notification_channel_value      TEXT NOT NULL,
                    primary key(uniqueness,notification_type,notification_channel,notification_channel_value));
CREATE INDEX IF NOT EXISTS sent_announcements_sent_at_ix ON sent_announcements (sent_at);
--************************************************************************************************************************************
-- scheduled_announcements
CREATE TABLE IF NOT EXISTS scheduled_announcements (
                    i                               BIGINT generated always as identity NOT NULL,
                    scheduled_at                    TIMESTAMP NOT NULL,
                    scheduled_for                   TIMESTAMP NOT NULL,
                    data                            JSONB NOT NULL,
                    language                        TEXT NOT NULL,
                    uniqueness                      TEXT NOT NULL,
                    notification_type               TEXT NOT NULL,
                    notification_channel            TEXT NOT NULL,
                    notification_channel_value      TEXT NOT NULL,
                    primary key(uniqueness,notification_type,notification_channel,notification_channel_value));
CREATE UNIQUE INDEX IF NOT EXISTS scheduled_announcements_i_ix ON scheduled_announcements (i);
CREATE INDEX IF NOT EXISTS scheduled_announcements_mod_i_ix ON scheduled_announcements (MOD(i, %[1]v),scheduled_for ASC);
--************************************************************************************************************************************
-- device_metadata
CREATE TABLE IF NOT EXISTS device_metadata (
                    user_id                     TEXT NOT NULL,
                    device_unique_id            TEXT NOT NULL,
                    push_notification_token     TEXT,
                    primary key(user_id, device_unique_id));

ALTER TABLE device_metadata DROP CONSTRAINT IF EXISTS device_metadata_user_id_fkey;

DO $$ BEGIN
    if exists(select * from pg_indexes where tablename = 'scheduled_notifications' AND indexname = 'scheduled_notifications_mod_i_ix'
                      AND indexdef = 'CREATE INDEX scheduled_notifications_mod_i_ix ON public.scheduled_notifications USING btree (mod(i, (%[1]v)::bigint), scheduled_for)'
    ) then
      DROP INDEX scheduled_notifications_mod_i_ix;
    end if;
    CREATE INDEX IF NOT EXISTS scheduled_notifications_mod_i_ix ON scheduled_notifications (MOD(i, %[1]v), scheduled_for, notification_channel ASC);
END $$;
