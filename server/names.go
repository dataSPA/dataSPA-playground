package server

import (
	"fmt"
	"math/rand/v2"
)

var adjectives = []string{
	"swift", "clever", "brave", "calm", "eager",
	"bold", "bright", "cool", "daring", "fancy",
	"gentle", "happy", "jolly", "keen", "lively",
	"merry", "noble", "proud", "quick", "sharp",
	"witty", "zesty", "vivid", "steady", "silent",
}

var nouns = []string{
	"fox", "owl", "hawk", "bear", "wolf",
	"deer", "hare", "lynx", "crow", "wren",
	"otter", "finch", "pike", "moth", "newt",
	"crane", "dove", "seal", "toad", "vole",
	"raven", "stoat", "shrew", "robin", "swift",
}

func RandomUsername() string {
	adj := adjectives[rand.IntN(len(adjectives))]
	noun := nouns[rand.IntN(len(nouns))]
	num := rand.IntN(100)
	return fmt.Sprintf("%s-%s-%d", adj, noun, num)
}
