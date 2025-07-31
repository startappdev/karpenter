/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package oci

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/samber/lo"
)

// PricingProvider handles OCI pricing information
type PricingProvider struct {
	client      *Client
	cache       map[string]priceEntry
	mu          sync.RWMutex
	lastUpdated time.Time
}

type priceEntry struct {
	price      float64
	expiration time.Time
}

// NewPricingProvider creates a new pricing provider
func NewPricingProvider(client *Client) *PricingProvider {
	return &PricingProvider{
		client: client,
		cache:  make(map[string]priceEntry),
	}
}

// GetShapePrice returns the price for a shape configuration
func (p *PricingProvider) GetShapePrice(ctx context.Context, shape string, ocpus int32, memoryGB int32, isPreemptible bool) (float64, error) {
	// Check cache first
	cacheKey := p.buildCacheKey(shape, ocpus, memoryGB, isPreemptible)
	
	p.mu.RLock()
	entry, found := p.cache[cacheKey]
	p.mu.RUnlock()
	
	if found && entry.expiration.After(time.Now()) {
		return entry.price, nil
	}

	// Calculate price based on OCI pricing model
	var hourlyPrice float64
	
	if isFlexibleShape(shape) {
		// Flexible shape pricing
		basePrice := p.getFlexibleShapeBasePrice(shape)
		ocpuPrice := basePrice.OCPUPerHour * float64(ocpus)
		memoryPrice := basePrice.MemoryGBHour * float64(memoryGB)
		hourlyPrice = ocpuPrice + memoryPrice
		
		// Apply preemptible discount
		if isPreemptible {
			hourlyPrice = hourlyPrice * 0.5 // 50% discount for preemptible
		}
	} else {
		// Fixed shape pricing
		hourlyPrice = p.getFixedShapePrice(shape)
		if isPreemptible {
			hourlyPrice = hourlyPrice * 0.5
		}
	}

	// Cache the result
	p.mu.Lock()
	p.cache[cacheKey] = priceEntry{
		price:      hourlyPrice,
		expiration: time.Now().Add(time.Hour),
	}
	p.mu.Unlock()
	
	return hourlyPrice, nil
}

// GetOptimalShapeConfiguration returns the most cost-effective shape configuration
func (p *PricingProvider) GetOptimalShapeConfiguration(ctx context.Context, minOCPUs, maxOCPUs, minMemoryGB, maxMemoryGB int32, allowedShapes []string) (*ShapeConfig, string, error) {
	type candidate struct {
		shape    string
		ocpus    int32
		memoryGB int32
		price    float64
	}
	
	var candidates []candidate
	
	for _, shape := range allowedShapes {
		if !isFlexibleShape(shape) {
			continue
		}
		
		// Get shape limits
		shapeLimits := p.getShapeLimits(shape)
		
		// Calculate optimal configuration within constraints
		ocpus := max(minOCPUs, shapeLimits.OCPUOptions.Min)
		ocpus = min(ocpus, min(maxOCPUs, shapeLimits.OCPUOptions.Max))
		
		memoryGB := max(minMemoryGB, shapeLimits.MemoryOptions.MinInGBs)
		memoryGB = min(memoryGB, min(maxMemoryGB, shapeLimits.MemoryOptions.MaxInGBs))
		
		// Calculate price
		price, err := p.GetShapePrice(ctx, shape, ocpus, memoryGB, false)
		if err != nil {
			continue
		}
		
		candidates = append(candidates, candidate{
			shape:    shape,
			ocpus:    ocpus,
			memoryGB: memoryGB,
			price:    price,
		})
	}
	
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("no valid shape configurations found")
	}
	
	// Find the cheapest option
	optimal := lo.MinBy(candidates, func(a, b candidate) bool {
		return a.price < b.price
	})
	
	return &ShapeConfig{
		OCPUs:       &optimal.ocpus,
		MemoryInGBs: &optimal.memoryGB,
	}, optimal.shape, nil
}

// EstimateMonthlyCost estimates the monthly cost for a shape configuration
func (p *PricingProvider) EstimateMonthlyCost(hourlyRate float64) float64 {
	// 730 hours in an average month
	return hourlyRate * 730
}

// CalculateSavings calculates percentage saved vs on-demand
func (p *PricingProvider) CalculateSavings(preemptiblePrice, onDemandPrice float64) int32 {
	if onDemandPrice == 0 {
		return 0
	}
	savings := ((onDemandPrice - preemptiblePrice) / onDemandPrice) * 100
	return int32(savings)
}

// Helper methods

func (p *PricingProvider) buildCacheKey(shape string, ocpus, memoryGB int32, isPreemptible bool) string {
	return fmt.Sprintf("%s-%d-%d-%v", shape, ocpus, memoryGB, isPreemptible)
}

func (p *PricingProvider) getFlexibleShapeBasePrice(shape string) PricingInfo {
	// In real implementation, this would fetch from OCI pricing API
	// For now, return example pricing
	
	switch shape {
	case "VM.Standard.E4.Flex":
		return PricingInfo{
			OCPUPerHour:  0.025,  // $0.025 per OCPU per hour
			MemoryGBHour: 0.0034, // $0.0034 per GB per hour
			Currency:     "USD",
		}
	case "VM.Standard.E5.Flex":
		return PricingInfo{
			OCPUPerHour:  0.030,
			MemoryGBHour: 0.0040,
			Currency:     "USD",
		}
	case "VM.Standard.A1.Flex":
		// ARM-based, typically cheaper
		return PricingInfo{
			OCPUPerHour:  0.01,
			MemoryGBHour: 0.0015,
			Currency:     "USD",
		}
	default:
		// Default pricing
		return PricingInfo{
			OCPUPerHour:  0.025,
			MemoryGBHour: 0.0034,
			Currency:     "USD",
		}
	}
}

func (p *PricingProvider) getFixedShapePrice(shape string) float64 {
	// In real implementation, this would fetch from OCI pricing API
	// For now, return example pricing
	
	fixedPrices := map[string]float64{
		"VM.Standard2.1":  0.0578,
		"VM.Standard2.2":  0.1156,
		"VM.Standard2.4":  0.2312,
		"VM.Standard2.8":  0.4624,
		"VM.Standard2.16": 0.9248,
		"VM.Standard2.24": 1.3872,
	}
	
	if price, ok := fixedPrices[shape]; ok {
		return price
	}
	
	return 0.10 // Default price
}

func (p *PricingProvider) getShapeLimits(shape string) Shape {
	// In real implementation, this would be fetched from shape details
	// For now, return known limits
	
	switch shape {
	case "VM.Standard.E4.Flex":
		return Shape{
			Name:       shape,
			IsFlexible: true,
			OCPUOptions: &OCPUOptions{
				Min: 1,
				Max: 64,
			},
			MemoryOptions: &MemoryOptions{
				MinInGBs:            1,
				MaxInGBs:            1024,
				DefaultPerOCPUInGBs: 16,
				MinPerOCPUInGBs:     1,
				MaxPerOCPUInGBs:     64,
			},
		}
	case "VM.Standard.E5.Flex":
		return Shape{
			Name:       shape,
			IsFlexible: true,
			OCPUOptions: &OCPUOptions{
				Min: 1,
				Max: 94,
			},
			MemoryOptions: &MemoryOptions{
				MinInGBs:            1,
				MaxInGBs:            1504,
				DefaultPerOCPUInGBs: 16,
				MinPerOCPUInGBs:     1,
				MaxPerOCPUInGBs:     64,
			},
		}
	case "VM.Standard.A1.Flex":
		return Shape{
			Name:       shape,
			IsFlexible: true,
			OCPUOptions: &OCPUOptions{
				Min: 1,
				Max: 80,
			},
			MemoryOptions: &MemoryOptions{
				MinInGBs:            1,
				MaxInGBs:            512,
				DefaultPerOCPUInGBs: 6,
				MinPerOCPUInGBs:     1,
				MaxPerOCPUInGBs:     64,
			},
		}
	default:
		// Default flexible shape limits
		return Shape{
			Name:       shape,
			IsFlexible: true,
			OCPUOptions: &OCPUOptions{
				Min: 1,
				Max: 32,
			},
			MemoryOptions: &MemoryOptions{
				MinInGBs:            1,
				MaxInGBs:            512,
				DefaultPerOCPUInGBs: 16,
				MinPerOCPUInGBs:     1,
				MaxPerOCPUInGBs:     64,
			},
		}
	}
}

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}