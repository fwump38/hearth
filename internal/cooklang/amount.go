package cooklang

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	reDec   = regexp.MustCompile(`^\d+(?:\.\d+)?$`)
	reFrac  = regexp.MustCompile(`^(\d+)\s*/\s*(\d+)$`)
	reMixed = regexp.MustCompile(`^(\d+)\s+(\d+)\s*/\s*(\d+)$`)
)

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if reDec.MatchString(s) {
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	}
	if m := reFrac.FindStringSubmatch(s); m != nil {
		a, _ := strconv.ParseFloat(m[1], 64)
		b, _ := strconv.ParseFloat(m[2], 64)
		if b == 0 {
			return 0, false
		}
		return a / b, true
	}
	if m := reMixed.FindStringSubmatch(s); m != nil {
		w, _ := strconv.ParseFloat(m[1], 64)
		a, _ := strconv.ParseFloat(m[2], 64)
		b, _ := strconv.ParseFloat(m[3], 64)
		if b == 0 {
			return 0, false
		}
		return w + a/b, true
	}
	return 0, false
}

// ParseAmount parses a quantity string such as "1 1/2", "2-3", "=1", "a pinch".
func ParseAmount(s string) Amount {
	s = strings.TrimSpace(s)
	var a Amount
	if strings.HasPrefix(s, "=") {
		a.Fixed = true
		s = strings.TrimSpace(s[1:])
	}
	if s == "" {
		return a
	}
	if f, ok := parseNumber(s); ok {
		a.Min, a.Max, a.HasNum = f, f, true
		return a
	}
	if i := strings.Index(s, "-"); i > 0 {
		lo, ok1 := parseNumber(s[:i])
		hi, ok2 := parseNumber(s[i+1:])
		if ok1 && ok2 {
			a.Min, a.Max, a.HasNum, a.IsRange = lo, hi, true, true
			return a
		}
	}
	a.Text = s
	return a
}

var fractions = []struct {
	v float64
	s string
}{
	{0.125, "⅛"}, {1.0 / 6, "⅙"}, {0.25, "¼"}, {1.0 / 3, "⅓"}, {0.375, "⅜"}, {0.5, "½"},
	{0.625, "⅝"}, {2.0 / 3, "⅔"}, {0.75, "¾"}, {5.0 / 6, "⅚"}, {0.875, "⅞"},
}

// FormatNumber renders a number using vulgar fractions where close.
func FormatNumber(f float64) string {
	if f < 0 {
		return "-" + FormatNumber(-f)
	}
	whole := math.Floor(f + 1e-9)
	frac := f - whole
	if frac < 0.02 {
		return strconv.FormatInt(int64(whole), 10)
	}
	if frac > 0.98 {
		return strconv.FormatInt(int64(whole)+1, 10)
	}
	for _, fr := range fractions {
		if math.Abs(frac-fr.v) < 0.02 {
			if whole == 0 {
				return fr.s
			}
			return fmt.Sprintf("%d%s", int64(whole), fr.s)
		}
	}
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

// Format renders the amount scaled by factor.
func (a Amount) Format(factor float64) string {
	if !a.HasNum {
		return a.Text
	}
	if a.Fixed || factor == 0 {
		factor = 1
	}
	if a.IsRange {
		return FormatNumber(a.Min*factor) + "–" + FormatNumber(a.Max*factor)
	}
	return FormatNumber(a.Min * factor)
}

// Scaled returns a copy multiplied by factor (unless fixed).
func (a Amount) Scaled(factor float64) Amount {
	if a.Fixed || !a.HasNum {
		return a
	}
	a.Min *= factor
	a.Max *= factor
	return a
}

// Quantity formats amount + unit for display ("1½ cup"), empty when no amount.
func Quantity(a Amount, unit string, factor float64) string {
	s := a.Format(factor)
	if s == "" {
		return ""
	}
	if unit != "" {
		s += " " + unit
	}
	return s
}

var unitCanon = map[string]string{
	"tablespoon": "tbsp", "tablespoons": "tbsp", "tbsps": "tbsp", "tbs": "tbsp", "tbl": "tbsp",
	"teaspoon": "tsp", "teaspoons": "tsp", "tsps": "tsp",
	"cups": "cup", "ounce": "oz", "ounces": "oz", "pound": "lb", "pounds": "lb", "lbs": "lb",
	"grams": "g", "gram": "g", "kilograms": "kg", "kilogram": "kg",
	"milliliters": "ml", "milliliter": "ml", "liters": "l", "liter": "l",
	"cloves": "clove", "cans": "can", "pinches": "pinch", "slices": "slice",
}

// CanonUnit normalizes a unit for merging quantities.
func CanonUnit(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	if c, ok := unitCanon[u]; ok {
		return c
	}
	if len(u) > 3 && strings.HasSuffix(u, "s") && !strings.HasSuffix(u, "ss") {
		return u[:len(u)-1]
	}
	return u
}
