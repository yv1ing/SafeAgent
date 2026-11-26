package pipeline

import (
	"strconv"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestUpstreamClientsStayBounded(t *testing.T) {
	clients := newUpstreamClients()
	for index := 0; index < maximumUpstreamClients+16; index++ {
		clients.get(&entity.Channel{ID: "ch_" + strconv.Itoa(index), Version: 1})
	}
	if len(clients.clients) != maximumUpstreamClients || len(clients.current) != maximumUpstreamClients {
		t.Fatalf("client cache is unbounded: %d clients, %d channels", len(clients.clients), len(clients.current))
	}
	if _, found := clients.current["ch_0"]; found {
		t.Fatal("the least recently used channel must be the one evicted")
	}
	if clients.get(&entity.Channel{ID: "ch_0", Version: 2}) == nil {
		t.Fatal("an evicted channel must get a new client")
	}
	if len(clients.clients) != maximumUpstreamClients {
		t.Fatalf("rebuilding an evicted channel grew the cache to %d clients", len(clients.clients))
	}
}
