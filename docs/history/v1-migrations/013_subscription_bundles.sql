-- Existing subscriptions remain restricted to their original user and token.
CREATE TABLE subscriptions_next(id INTEGER PRIMARY KEY, name TEXT NOT NULL, format TEXT NOT NULL CHECK(format IN ('v2ray','mihomo','singbox')), token_hash TEXT NOT NULL UNIQUE, user_id INTEGER REFERENCES users(id), enabled INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, selection_scope TEXT NOT NULL DEFAULT 'user' CHECK(selection_scope IN ('user','bundle')), CHECK((selection_scope='user' AND user_id IS NOT NULL) OR (selection_scope='bundle' AND user_id IS NULL)));
CREATE TABLE subscription_nodes_next(subscription_id INTEGER NOT NULL REFERENCES subscriptions_next(id) ON DELETE CASCADE, node_id TEXT NOT NULL REFERENCES nodes(id), position INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(subscription_id,node_id));
INSERT INTO subscriptions_next SELECT id,name,format,token_hash,user_id,enabled,created_at,updated_at,'user' FROM subscriptions;
INSERT INTO subscription_nodes_next SELECT subscription_id,node_id,position FROM subscription_nodes;
DROP TABLE subscription_nodes;
DROP TABLE subscriptions;
ALTER TABLE subscriptions_next RENAME TO subscriptions;
ALTER TABLE subscription_nodes_next RENAME TO subscription_nodes;
