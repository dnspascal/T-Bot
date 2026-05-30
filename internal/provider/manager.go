package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Manager orchestrates multiple providers for parallel trading
type Manager struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

// NewManager creates a new provider manager
func NewManager() *Manager {
	return &Manager{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the manager
func (m *Manager) Register(name string, prov Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.providers[name]; exists {
		return fmt.Errorf("provider %s already registered", name)
	}

	m.providers[name] = prov
	slog.Info("provider registered", "name", name)
	return nil
}

// GetProvider returns a provider by name
func (m *Manager) GetProvider(name string) (Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prov, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return prov, nil
}

// ListProviders returns all registered provider names
func (m *Manager) ListProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for name := range m.providers {
		names = append(names, name)
	}
	return names
}

// AuthAllProviders authenticates all registered providers in parallel
func (m *Manager) AuthAllProviders(ctx context.Context) (map[string]*AuthResult, error) {
	m.mu.RLock()
	providers := make([]Provider, 0, len(m.providers))
	providerNames := make([]string, 0, len(m.providers))
	for name, prov := range m.providers {
		providers = append(providers, prov)
		providerNames = append(providerNames, name)
	}
	m.mu.RUnlock()

	results := make(map[string]*AuthResult)
	resultsCh := make(chan struct {
		name   string
		result *AuthResult
		err    error
	}, len(providers))

	var wg sync.WaitGroup
	for i, prov := range providers {
		wg.Add(1)
		go func(idx int, p Provider) {
			defer wg.Done()
			result, err := p.Auth(ctx)
			resultsCh <- struct {
				name   string
				result *AuthResult
				err    error
			}{
				name:   providerNames[idx],
				result: result,
				err:    err,
			}
		}(i, prov)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var errs []error
	for res := range resultsCh {
		if res.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", res.name, res.err))
			slog.Error("provider auth failed", "name", res.name, "err", res.err)
		} else {
			results[res.name] = res.result
			slog.Info("provider authenticated", "name", res.name, "balance", res.result.Balance)
		}
	}

	if len(errs) > 0 {
		return results, fmt.Errorf("auth errors: %v", errs)
	}

	return results, nil
}

// SetupAllProviders calls Setup on all registered providers in parallel
func (m *Manager) SetupAllProviders(ctx context.Context) error {
	m.mu.RLock()
	providers := make(map[string]Provider)
	for name, prov := range m.providers {
		providers[name] = prov
	}
	m.mu.RUnlock()

	setupCh := make(chan struct {
		name string
		err  error
	}, len(providers))

	var wg sync.WaitGroup
	for name, prov := range providers {
		wg.Add(1)
		go func(n string, p Provider) {
			defer wg.Done()
			err := p.Setup()
			setupCh <- struct {
				name string
				err  error
			}{
				name: n,
				err:  err,
			}
		}(name, prov)
	}

	go func() {
		wg.Wait()
		close(setupCh)
	}()

	var errs []error
	for res := range setupCh {
		if res.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", res.name, res.err))
			slog.Error("provider setup failed", "name", res.name, "err", res.err)
		} else {
			slog.Info("provider setup complete", "name", res.name)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("setup errors: %v", errs)
	}

	return nil
}

// CloseAllProviders closes all registered providers in parallel
func (m *Manager) CloseAllProviders() error {
	m.mu.RLock()
	providers := make([]Provider, 0, len(m.providers))
	for _, prov := range m.providers {
		providers = append(providers, prov)
	}
	m.mu.RUnlock()

	closeCh := make(chan error, len(providers))

	var wg sync.WaitGroup
	for _, prov := range providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			closeCh <- p.Close()
		}(prov)
	}

	go func() {
		wg.Wait()
		close(closeCh)
	}()

	var errs []error
	for err := range closeCh {
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}

// PlaceOrderAcrossProviders places an order on all registered providers
// Returns a map of provider name to order ID
func (m *Manager) PlaceOrderAcrossProviders(
	ctx context.Context,
	side string,
	volume int64,
	slPips float64,
	tpPips float64,
) (map[string]string, error) {
	m.mu.RLock()
	providers := make(map[string]Provider)
	for name, prov := range m.providers {
		providers[name] = prov
	}
	m.mu.RUnlock()

	orderIDsCh := make(chan struct {
		providerName string
		orderID      string
		err          error
	}, len(providers))

	var wg sync.WaitGroup
	for name, prov := range providers {
		wg.Add(1)
		go func(n string, p Provider) {
			defer wg.Done()
			orderID, err := p.PlaceMarketOrder(ctx, side, volume, slPips, tpPips)
			orderIDsCh <- struct {
				providerName string
				orderID      string
				err          error
			}{
				providerName: n,
				orderID:      orderID,
				err:          err,
			}
		}(name, prov)
	}

	go func() {
		wg.Wait()
		close(orderIDsCh)
	}()

	orderIDs := make(map[string]string)
	var errs []error
	for res := range orderIDsCh {
		if res.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", res.providerName, res.err))
			slog.Error("order placement failed", "provider", res.providerName, "err", res.err)
		} else {
			orderIDs[res.providerName] = res.orderID
			slog.Info("order placed", "provider", res.providerName, "orderID", res.orderID)
		}
	}

	if len(errs) > 0 {
		return orderIDs, fmt.Errorf("order errors: %v", errs)
	}

	return orderIDs, nil
}

// FetchAccountInfoAcrossProviders fetches account info from all providers in parallel
func (m *Manager) FetchAccountInfoAcrossProviders(ctx context.Context) (map[string]*AccountInfo, error) {
	m.mu.RLock()
	providers := make(map[string]Provider)
	for name, prov := range m.providers {
		providers[name] = prov
	}
	m.mu.RUnlock()

	accountInfoCh := make(chan struct {
		providerName string
		info         *AccountInfo
		err          error
	}, len(providers))

	var wg sync.WaitGroup
	for name, prov := range providers {
		wg.Add(1)
		go func(n string, p Provider) {
			defer wg.Done()
			info, err := p.FetchAccountInfo(ctx)
			accountInfoCh <- struct {
				providerName string
				info         *AccountInfo
				err          error
			}{
				providerName: n,
				info:         info,
				err:          err,
			}
		}(name, prov)
	}

	go func() {
		wg.Wait()
		close(accountInfoCh)
	}()

	accountInfos := make(map[string]*AccountInfo)
	var errs []error
	for res := range accountInfoCh {
		if res.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", res.providerName, res.err))
			slog.Error("account info fetch failed", "provider", res.providerName, "err", res.err)
		} else {
			accountInfos[res.providerName] = res.info
			slog.Info("account info fetched", "provider", res.providerName, "balance", res.info.Balance)
		}
	}

	if len(errs) > 0 {
		return accountInfos, fmt.Errorf("account info errors: %v", errs)
	}

	return accountInfos, nil
}
