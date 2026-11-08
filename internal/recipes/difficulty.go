package recipes

// ComputeDifficulty ports the formula from the recipe-to-obsidian skill.
//
//	score = 0.3*steps + 0.6*cookware + active/30  (-0.7 for one-pot style tags)
//	active = prep + min(cook, 60), capped at 120
//	Hard if score >= 8.5 && steps >= 8; Easy if score < 4.4; else Medium.
func ComputeDifficulty(steps, cookware, prepMin, cookMin int, tags []string) string {
	c := cookMin
	if c > 60 {
		c = 60
	}
	active := prepMin + c
	if active > 120 {
		active = 120
	}
	score := 0.3*float64(steps) + 0.6*float64(cookware) + float64(active)/30
	for _, t := range tags {
		switch t {
		case "one-pot", "sheet-pan", "no-cook", "no-bake", "slow-cooker":
			score -= 0.7
		default:
			continue
		}
		break
	}
	switch {
	case score >= 8.5 && steps >= 8:
		return "Hard"
	case score < 4.4:
		return "Easy"
	}
	return "Medium"
}
