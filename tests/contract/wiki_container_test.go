package contract

import "os"

func wikiContainer() string {
	if container := os.Getenv("PETAR_TEST_WIKI_CONTAINER"); container != "" {
		return container
	}
	return "dokuwiki"
}
