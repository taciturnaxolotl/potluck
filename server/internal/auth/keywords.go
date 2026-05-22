package auth

// keyWords is the curated mnemonic word list for API key generation.
//
// 256 single-and-two-syllable words drawn from herbs, plants, materials,
// minerals, weather, and small landscape features. Picked for being:
//
//   - visually distinct (no rhymes / look-alikes)
//   - safe-for-work
//   - easy to say out loud over a call
//   - not a brand name, person, or potentially-trademarked term
//
// The word is NOT a security input. It exists so a user can refer to
// "my cedar key" and find it in the UI without copy-pasting entropy.
// Adding to this list is fine; never remove a word (existing keys
// reference them by exact match for display purposes).
var keyWords = [...]string{
	"acorn", "agate", "alder", "almond", "amber", "amethyst", "apricot", "arrow",
	"ash", "aspen", "azure", "balsa", "bamboo", "barley", "basil", "bay",
	"beech", "beet", "berry", "birch", "bison", "blossom", "blueberry", "boulder",
	"branch", "brass", "bread", "breeze", "brick", "bronze", "brook", "broom",
	"buckwheat", "cabbage", "cactus", "cardamom", "carrot", "cedar", "celery", "cherry",
	"chervil", "chestnut", "chicory", "chive", "cinder", "citrine", "clay", "clematis",
	"clove", "clover", "coal", "cobalt", "comet", "comfrey", "copper", "coral",
	"cork", "cosmos", "cotton", "cove", "cranberry", "cress", "crimson", "crystal",
	"cumin", "currant", "cypress", "daffodil", "dahlia", "daisy", "dawn", "dew",
	"dill", "dogwood", "drift", "dune", "eelgrass", "elder", "elm", "ember",
	"emerald", "endive", "fennel", "fern", "fig", "fjord", "flax", "flint",
	"fog", "frost", "garlic", "gem", "ginger", "glacier", "gold", "gorse",
	"grain", "granite", "grass", "gravel", "grove", "hawthorn", "hazel", "heath",
	"heather", "hemlock", "henna", "hickory", "honey", "hops", "hornbeam", "horsetail",
	"iris", "iron", "ivory", "ivy", "jade", "jasmine", "jasper", "juniper",
	"kale", "kelp", "lace", "lake", "larch", "lark", "laurel", "lavender",
	"leek", "lemon", "lichen", "lilac", "lily", "lime", "linden", "lotus",
	"mallow", "maple", "marble", "marigold", "marjoram", "marsh", "meadow", "mesa",
	"millet", "mineral", "mint", "mist", "moonstone", "morel", "moss", "mulberry",
	"mustard", "myrtle", "nettle", "nutmeg", "oak", "oat", "olive", "onyx",
	"opal", "orchard", "oregano", "papyrus", "parsley", "parsnip", "pebble", "peony",
	"pepper", "pine", "pistachio", "plum", "pollen", "pond", "poplar", "poppy",
	"prairie", "primrose", "pumpkin", "quartz", "quince", "raddish", "raffia", "rain",
	"raspberry", "redwood", "reed", "rhubarb", "ribbon", "ridge", "river", "rose",
	"rosemary", "ruby", "rush", "saffron", "sage", "sand", "sapphire", "savory",
	"sedge", "shale", "shell", "silver", "slate", "snow", "sorrel", "spelt",
	"spice", "sprig", "spruce", "starflower", "steel", "stone", "stream", "sumac",
	"sunflower", "sycamore", "tangerine", "tarragon", "teal", "thistle", "thorn", "thyme",
	"tide", "tinder", "topaz", "trout", "tulip", "tundra", "turmeric", "twilight",
	"vanilla", "verbena", "vetch", "viburnum", "vine", "violet", "walnut", "watercress",
	"wax", "wheat", "whisk", "willow", "winter", "wisteria", "wood", "wool",
	"yarrow", "yew", "zinc", "amber", "barn", "bean", "blue", "bough",
	"chai", "fawn", "frog", "kite", "leaf", "moor", "owl", "swan",
}

// keyWordIndex is keyWords as a map for O(1) lookup during validation.
var keyWordIndex map[string]struct{}

func init() {
	keyWordIndex = make(map[string]struct{}, len(keyWords))
	for _, w := range keyWords {
		keyWordIndex[w] = struct{}{}
	}
}
