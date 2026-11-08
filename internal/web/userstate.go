package web

import "database/sql"

// userState is a per-request snapshot of a user's favorites and tried flags.
type userState struct{ fav, tried map[string]bool }

func (u userState) Favorite(s string) bool { return u.fav[s] }
func (u userState) Tried(s string) bool    { return u.tried[s] }

func loadSet(db *sql.DB, table string, uid int64) map[string]bool {
	out := map[string]bool{}
	rows, err := db.Query(`SELECT slug FROM `+table+` WHERE user_id=?`, uid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out[s] = true
		}
	}
	return out
}

func loadUserState(db *sql.DB, uid int64) userState {
	return userState{fav: loadSet(db, "favorites", uid), tried: loadSet(db, "tried", uid)}
}

// toggle flips membership in favorites/tried and returns the new value.
func toggle(db *sql.DB, table string, uid int64, slug string) (bool, error) {
	var one int
	err := db.QueryRow(`SELECT 1 FROM `+table+` WHERE user_id=? AND slug=?`, uid, slug).Scan(&one)
	if err == sql.ErrNoRows {
		_, err = db.Exec(`INSERT INTO `+table+`(user_id,slug) VALUES(?,?)`, uid, slug)
		return true, err
	}
	if err != nil {
		return false, err
	}
	_, err = db.Exec(`DELETE FROM `+table+` WHERE user_id=? AND slug=?`, uid, slug)
	return false, err
}
