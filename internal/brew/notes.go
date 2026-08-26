package brew

import "strings"

// noteCategories maps the words a customer uses to the words a shop writes on
// its bags.
//
// A request and a tasting note are written by different people for different
// purposes. Somebody asks for "something floral"; the roaster wrote "jasmine,
// bergamot". Matching those as plain text finds nothing, and a recommendation
// tool that cannot connect them is a search box with extra steps.
//
// The groupings follow how a flavour wheel is read: a broad term at the centre,
// the specific descriptors a taster actually writes further out.
var noteCategories = map[string][]string{
	"floral":     {"jasmine", "bergamot", "rose", "lavender", "elderflower", "hibiscus", "orange blossom", "chamomile"},
	"fruity":     {"berry", "blueberry", "strawberry", "raspberry", "stone fruit", "peach", "apricot", "cherry", "plum", "apple", "grape", "tropical", "mango", "papaya", "pineapple", "citrus"},
	"berry":      {"blueberry", "strawberry", "raspberry", "blackcurrant", "blackberry", "cranberry"},
	"citrus":     {"orange", "lemon", "lime", "grapefruit", "bergamot", "tangerine", "mandarin"},
	"stonefruit": {"peach", "apricot", "nectarine", "plum", "cherry"},
	"chocolate":  {"cocoa", "cacao", "cocoa nib", "dark chocolate", "milk chocolate", "fudge", "brownie"},
	"nutty":      {"almond", "hazelnut", "peanut", "walnut", "pecan", "cashew"},
	"sweet":      {"caramel", "honey", "molasses", "brown sugar", "syrup", "syrupy", "toffee", "vanilla", "maple"},
	"spice":      {"cinnamon", "clove", "cardamom", "nutmeg", "pepper", "anise"},
	"bright":     {"citrus", "lime", "lemon", "orange", "bergamot", "grapefruit", "blackcurrant", "green apple", "red apple", "tomato", "cranberry"},
	"winey":      {"grape", "wine", "fermented", "boozy", "rum"},
	"earthy":     {"tobacco", "cedar", "leather", "mushroom", "forest floor"},
	"tea":        {"black tea", "green tea", "bergamot", "jasmine", "earl grey"},
	"savoury":    {"tomato", "herbal", "olive", "brothy"},
}

// acidityWords all mean the same request: a cup that tastes lively. They point
// at the same group of descriptors as "bright".
var acidityWords = map[string]bool{
	"bright": true, "acidity": true, "acidic": true, "acid": true,
	"lively": true, "juicy": true, "sharp": true, "tart": true, "crisp": true,
}

// expand returns the descriptors a request term should be matched against,
// including the term itself.
func expand(term string) []string {
	term = normalise(term)
	if term == "" {
		return nil
	}

	if acidityWords[term] {
		return append([]string{term}, noteCategories["bright"]...)
	}
	// "stone fruit" is written both ways; the key is stored without the space.
	if members, ok := noteCategories[strings.ReplaceAll(term, " ", "")]; ok {
		return append([]string{term}, members...)
	}
	if members, ok := noteCategories[term]; ok {
		return append([]string{term}, members...)
	}
	return []string{term}
}

// normalise lowercases a term and strips the words that carry no flavour.
//
// A request arrives as somebody said it — "bright acidity", "quite floral" —
// and the filler has to come off before the rest can be matched.
func normalise(term string) string {
	term = strings.ToLower(strings.TrimSpace(term))

	for _, filler := range []string{"notes of", "hints of", "a bit ", "quite ", "very ", "really ", "pronounced ", "some "} {
		term = strings.ReplaceAll(term, filler, "")
	}
	return strings.TrimSpace(term)
}

// termsOf breaks a request into the terms worth matching.
//
// A phrase is tried whole first, because "stone fruit" and "black tea" mean
// something their separate words do not, and only then word by word so that
// "bright acidity" still finds a lively coffee.
func termsOf(request string) []string {
	request = normalise(request)
	if request == "" {
		return nil
	}

	terms := []string{request}
	if words := strings.Fields(request); len(words) > 1 {
		for _, word := range words {
			if len(word) > 2 && !isFiller(word) {
				terms = append(terms, word)
			}
		}
	}
	return terms
}

func isFiller(word string) bool {
	switch word {
	case "and", "with", "the", "for", "but", "not", "too":
		return true
	default:
		return false
	}
}
