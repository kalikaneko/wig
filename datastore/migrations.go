package datastore

import "git.autistici.org/ai3/attic/wig/datastore/sqlite"

var Migrations = []sqlite.Migration{
	sqlite.Statement(`
CREATE TABLE log (
  seq INTEGER PRIMARY KEY NOT NULL,
  type INTEGER NOT NULL,
  value TEXT,
  timestamp DATETIME
);
`, `
CREATE TABLE interfaces (
  name SMALLTEXT PRIMARY KEY NOT NULL,
  port INTEGER,
  ip TEXT,
  ip6 TEXT,
  fwmark INTEGER,
  private_key TEXT,
  public_key TEXT
);
`, `
CREATE TABLE peers (
  public_key SMALLTEXT PRIMARY KEY NOT NULL,
  interface SMALLTEXT NOT NULL,
  ip TEXT,
  ip6 TEXT,
  expire DATETIME
);
`, `
CREATE INDEX idx_peers_ip ON peers(ip);
`, `
CREATE INDEX idx_peers_ip6 ON peers(ip6);
`, `
CREATE TABLE sequence (
  seq INTEGER
);
`, `
INSERT INTO sequence (seq) VALUES (0);
`, `
CREATE TABLE active_sessions (
  peer_public_key SMALLTEXT,
  begin_timestamp DATETIME
  end_timestamp DATETIME,
  last_handshake DATETIME,
  active BOOL,
  src_as TEXT,
  src_country TEXT
);
`, `
CREATE TABLE sessions (
  peer_public_key SMALLTEXT,
  begin_timestamp DATETIME,
  end_timestamp DATETIME,
  src_as TEXT,
  src_country TEXT
);
`, `
CREATE INDEX idx_sessions_peer ON sessions(peer_public_key);
`, `
CREATE TABLE tokens (
  id TEXT PRIMARY KEY NOT NULL,
  secret TEXT NOT NULL,
  roles TEXT
)
`),
}
