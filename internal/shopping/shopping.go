// Package shopping merges recipe ingredients into a grouped shopping list.
package shopping

import (
	"sort"
	"strings"

	"github.com/fwump38/hearth/internal/cooklang"
)

// Source is a recipe added to the list at some scale.
type Source struct {
	Title  string
	Recipe *cooklang.Recipe
	Factor float64
}

// Item is one line of the shopping list.
type Item struct {
	Key     string // lowercase name; identifies the item for check-off
	Name    string
	Qty     string
	From    []string
	Aisle   string
	Manual  bool
	Checked bool
	Pantry  bool
	ID      int64 // manual item id
}

// Group is a list section.
type Group struct {
	Aisle string
	Items []Item
}

// Manual is a hand-typed item.
type Manual struct {
	ID   int64
	Text string
}

// Sink receives list events. Implement it to mirror the list elsewhere
// (see docs/home-assistant-sync.md). The default is a no-op.
type Sink interface {
	ItemsChanged(groups []Group)
	ItemChecked(key string, checked bool)
}

// NoSink ignores everything.
type NoSink struct{}

func (NoSink) ItemsChanged([]Group)     {}
func (NoSink) ItemChecked(string, bool) {}

// Aisles maps ingredient names to store sections (aisle.conf format).
type Aisles struct {
	order []string
	by    map[string]string
}

// ParseAisles parses the Cooklang aisle.conf format:
//
//	[produce]
//	apples|apple
func ParseAisles(conf string) *Aisles {
	a := &Aisles{by: map[string]string{}}
	cur := ""
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			a.order = append(a.order, cur)
			continue
		}
		if cur == "" {
			continue
		}
		for _, syn := range strings.Split(line, "|") {
			if syn = strings.ToLower(strings.TrimSpace(syn)); syn != "" {
				a.by[syn] = cur
			}
		}
	}
	return a
}

// Lookup finds the aisle for an ingredient name ("" if unknown).
func (a *Aisles) Lookup(name string) string {
	if a == nil {
		return ""
	}
	n := strings.ToLower(strings.TrimSpace(name))
	if s, ok := a.by[n]; ok {
		return s
	}
	best, bestLen := "", 0
	for syn, aisle := range a.by {
		if len(syn) > bestLen && (strings.HasSuffix(n, " "+syn) || strings.HasPrefix(n, syn+" ") || strings.Contains(n, " "+syn+" ")) {
			best, bestLen = aisle, len(syn)
		}
	}
	return best
}

func (a *Aisles) rank(aisle string) int {
	if a != nil {
		for i, o := range a.order {
			if o == aisle {
				return i
			}
		}
	}
	return 1 << 20
}

// Build merges all sources and manual items into aisle groups.
// Optional ingredients are skipped; pantry items go into a final "Pantry" group.
func Build(sources []Source, manual []Manual, aisles *Aisles, pantry []string, checked map[string]bool) []Group {
	type acc struct {
		item Item
		qtys []cooklang.ListQty
	}
	pan := map[string]bool{}
	for _, p := range pantry {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			pan[p] = true
		}
	}
	items := map[string]*acc{}
	var order []string
	for _, src := range sources {
		for _, li := range src.Recipe.IngredientList() {
			if li.Optional {
				continue
			}
			key := strings.ToLower(li.Name)
			a, ok := items[key]
			if !ok {
				a = &acc{item: Item{Key: key, Name: li.Name, Pantry: pan[key]}}
				items[key] = a
				order = append(order, key)
			}
			if !containsStr(a.item.From, src.Title) {
				a.item.From = append(a.item.From, src.Title)
			}
			for _, q := range li.Qtys {
				q.Amount = q.Amount.Scaled(src.Factor)
				merged := false
				for i := range a.qtys {
					x := &a.qtys[i]
					if cooklang.CanonUnit(x.Unit) == cooklang.CanonUnit(q.Unit) && x.Amount.HasNum && q.Amount.HasNum {
						x.Amount.Min += q.Amount.Min
						x.Amount.Max += q.Amount.Max
						x.Amount.IsRange = x.Amount.IsRange || q.Amount.IsRange
						merged = true
						break
					}
				}
				if !merged {
					a.qtys = append(a.qtys, q)
				}
			}
		}
	}
	byAisle := map[string][]Item{}
	for _, key := range order {
		a := items[key]
		var parts []string
		for _, q := range a.qtys {
			if s := cooklang.Quantity(q.Amount, q.Unit, 1); s != "" {
				parts = append(parts, s)
			}
		}
		a.item.Qty = strings.Join(parts, " + ")
		a.item.Checked = checked[key]
		aisle := aisles.Lookup(a.item.Name)
		if aisle == "" {
			aisle = "Other"
		}
		if a.item.Pantry {
			aisle = "Pantry"
		}
		a.item.Aisle = aisle
		byAisle[aisle] = append(byAisle[aisle], a.item)
	}
	for _, m := range manual {
		key := "manual:" + strings.ToLower(m.Text)
		byAisle["Added by hand"] = append(byAisle["Added by hand"], Item{Key: key, Name: m.Text, Manual: true, ID: m.ID, Checked: checked[key], Aisle: "Added by hand"})
	}
	var names []string
	for n := range byAisle {
		names = append(names, n)
	}
	weight := func(n string) int {
		switch n {
		case "Added by hand":
			return 1 << 21
		case "Other":
			return 1 << 22
		case "Pantry":
			return 1 << 23
		}
		return aisles.rank(n)
	}
	sort.Slice(names, func(i, j int) bool {
		wi, wj := weight(names[i]), weight(names[j])
		if wi != wj {
			return wi < wj
		}
		return names[i] < names[j]
	})
	var out []Group
	for _, n := range names {
		its := byAisle[n]
		sort.SliceStable(its, func(i, j int) bool {
			if its[i].Checked != its[j].Checked {
				return !its[i].Checked
			}
			return strings.ToLower(its[i].Name) < strings.ToLower(its[j].Name)
		})
		out = append(out, Group{Aisle: n, Items: its})
	}
	return out
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// DefaultAisles is a starting aisle.conf.
const DefaultAisles = `[Produce]
apples|apple
asparagus
avocado|avocados
bananas|banana
basil
bell pepper|bell peppers|red bell pepper|green bell pepper
broccoli
cabbage
carrots|carrot
cauliflower
celery
cilantro
corn
cucumber
garlic
ginger|fresh ginger
jalapeño|jalapeno
kale
lemon|lemons
lime|limes
mushrooms
onion|onions|yellow onion|red onion|shallot|shallots|green onions|scallions
parsley
peas
potatoes|potato
sweet potato|sweet potatoes
spinach
tomatoes|tomato
zucchini
mint
thyme
rosemary

[Dairy & Eggs]
butter
cheese|cheddar|parmesan|mozzarella|feta
cream|heavy cream
eggs|egg
milk
sour cream
yogurt

[Meat & Seafood]
bacon
beef|ground beef
chicken
pork
sausage
shrimp
salmon
fish

[Bakery]
bread
tortillas|tortilla
flatbread
buns

[Pantry & Dry Goods]
flour|all-purpose flour
sugar|brown sugar|coconut sugar
rice|arborio rice
pasta|spaghetti|noodles
oats|rolled oats
beans|black beans|chickpeas|lentils
broth|stock|vegetable broth
oil|olive oil|vegetable oil|avocado oil|coconut oil
vinegar
soy sauce
baking powder|baking soda
vanilla|vanilla extract
chocolate|chocolate chips
nuts|almonds|pecans|walnuts|cashews

[Spices]
salt|sea salt
black pepper|pepper
cinnamon|ground cinnamon
cumin
paprika|smoked paprika
chili powder
oregano
nutmeg

[Canned & Jarred]
canned tomatoes|tomato paste|tomato sauce|salsa
coconut milk
hot sauce

[Frozen]
frozen peas
ice cream

[Drinks & Alcohol]
tequila
vodka
rum
gin
wine
beer
sparkling water|club soda|tonic
`

// DefaultPantry lists items assumed to be on hand.
const DefaultPantry = "salt\nsea salt\nblack pepper\nwater\nice"
