package console

import (
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/service/diagnosis"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Services is the set of use cases the console transport serves. Every field is constructed once
// at startup and shares the same store and selector.
type Services struct {
	Account    *AccountService
	Project    *ProjectService
	Model      *ModelService
	Channel    *ChannelService
	Key        *KeyService
	Route      *RouteService
	Rule       *RuleService
	Calls      *CallService
	Stats      *StatsService
	Operations *OperationService
	Analysis   *diagnosis.Service
	Policy     *PolicyService
	Monitor    *MonitorService
}

func NewServices(store *repository.Store, selector *routing.Selector, providers *provider.Registry, analyzer *diagnosis.Service) Services {
	return Services{
		Account:    NewAccountService(store),
		Project:    NewProjectService(store),
		Model:      NewModelService(store, providers),
		Channel:    NewChannelService(store, selector, providers),
		Key:        NewKeyService(store),
		Route:      NewRouteService(store),
		Rule:       NewRuleService(store),
		Calls:      NewCallService(store, analyzer),
		Stats:      NewStatsService(store, selector),
		Operations: NewOperationService(store),
		Analysis:   analyzer,
		Policy:     NewPolicyService(store),
		Monitor:    NewMonitorService(store),
	}
}
