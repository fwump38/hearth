CREATE TABLE user_prefs (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (user_id, key)
);
UPDATE views SET json = json_set(json, '$.show_home', json('true')) WHERE slug = 'table';
