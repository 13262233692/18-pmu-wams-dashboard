package control

import (
	"fmt"
	"log"
	"sync"
	"time"

	"wams-dashboard/internal/models"
)

type ControlExecutor struct {
	alertChan    <-chan *models.OscillationAlert
	controlChan  chan<- *models.ControlAction
	broadcastFunc func(*models.ControlAction)

	executedActions map[string]*models.ControlAction
	mu              sync.RWMutex

	stationGenerators map[string][]string
	stationBrakers    map[string][]string

	tripCapacityMW    map[string]float64
	brakeCapacityMW   map[string]float64

	cooldownPeriod time.Duration
	lastActionTime time.Time
	maxActionsPerHour int
	actionCountWindow []time.Time
	windowMu          sync.Mutex

	running  bool
	stopChan chan struct{}
	runMu    sync.Mutex
}

func NewControlExecutor(
	alertChan <-chan *models.OscillationAlert,
	controlChan chan<- *models.ControlAction,
	broadcastFunc func(*models.ControlAction),
) *ControlExecutor {
	exec := &ControlExecutor{
		alertChan:         alertChan,
		controlChan:       controlChan,
		broadcastFunc:     broadcastFunc,
		executedActions:   make(map[string]*models.ControlAction),
		cooldownPeriod:    30 * time.Second,
		maxActionsPerHour: 10,
		stopChan:          make(chan struct{}),
		stationGenerators: map[string][]string{
			"华东-换流站A": {"GEN-A1", "GEN-A2", "GEN-A3", "GEN-A4"},
			"华北-变电站B": {"GEN-B1", "GEN-B2"},
			"华中-枢纽站C": {"GEN-C1", "GEN-C2", "GEN-C3"},
			"西南-水电厂D": {"GEN-D1", "GEN-D2", "GEN-D3", "GEN-D4", "GEN-D5"},
			"西北-火电厂E": {"GEN-E1", "GEN-E2", "GEN-E3", "GEN-E4"},
			"东北-风电场F": {"GEN-F1", "GEN-F2"},
			"华南-核电G":   {"GEN-G1", "GEN-G2"},
			"山东-光伏H":   {"GEN-H1", "GEN-H2", "GEN-H3"},
		},
		stationBrakers: map[string][]string{
			"华东-换流站A": {"BRK-A1", "BRK-A2"},
			"华北-变电站B": {"BRK-B1"},
			"华中-枢纽站C": {"BRK-C1", "BRK-C2"},
			"西南-水电厂D": {"BRK-D1"},
			"西北-火电厂E": {"BRK-E1", "BRK-E2"},
			"东北-风电场F": {"BRK-F1"},
			"华南-核电G":   {"BRK-G1"},
			"山东-光伏H":   {"BRK-H1"},
		},
		tripCapacityMW: map[string]float64{
			"华东-换流站A": 3000,
			"华北-变电站B": 1500,
			"华中-枢纽站C": 2000,
			"西南-水电厂D": 4000,
			"西北-火电厂E": 2500,
			"东北-风电场F": 800,
			"华南-核电G":   3500,
			"山东-光伏H":   1000,
		},
		brakeCapacityMW: map[string]float64{
			"华东-换流站A": 600,
			"华北-变电站B": 300,
			"华中-枢纽站C": 400,
			"西南-水电厂D": 500,
			"西北-火电厂E": 450,
			"东北-风电场F": 200,
			"华南-核电G":   700,
			"山东-光伏H":   250,
		},
	}
	return exec
}

func (ce *ControlExecutor) Start() {
	ce.runMu.Lock()
	if ce.running {
		ce.runMu.Unlock()
		return
	}
	ce.running = true
	ce.runMu.Unlock()

	go ce.processAlerts()
}

func (ce *ControlExecutor) Stop() {
	close(ce.stopChan)
}

func (ce *ControlExecutor) processAlerts() {
	for {
		select {
		case <-ce.stopChan:
			return
		case alert := <-ce.alertChan:
			if alert == nil {
				continue
			}
			ce.handleAlert(alert)
		}
	}
}

func (ce *ControlExecutor) handleAlert(alert *models.OscillationAlert) {
	ce.mu.RLock()
	timeSinceLast := time.Since(ce.lastActionTime)
	isFirstAction := ce.lastActionTime.IsZero()
	cooldownOK := isFirstAction || timeSinceLast >= ce.cooldownPeriod
	if !cooldownOK {
		ce.mu.RUnlock()
		log.Printf("[CONTROL] Skipping action - in cooldown period (last action %v ago, isFirst=%v)",
			timeSinceLast, isFirstAction)
		return
	}
	ce.mu.RUnlock()

	if !ce.canIssueAction() {
		log.Printf("[CONTROL] Skipping action - rate limit exceeded (%d actions this hour)",
			len(ce.actionCountWindow))
		return
	}

	needsControl := false
	severity := alert.Severity

	switch {
	case severity == "EMERGENCY":
		needsControl = true
	case severity == "ALERT" && alert.Diverging:
		needsControl = true
	case severity == "ALERT" && alert.ConfidenceLevel > 0.6 && alert.DivergenceCount >= 5:
		needsControl = true
	}

	if !needsControl {
		log.Printf("[CONTROL] Alert severity '%s' does not require control action yet (conf=%.2f, div=%d)",
			severity, alert.ConfidenceLevel, alert.DivergenceCount)
		return
	}

	action := ce.buildControlAction(alert)
	if action == nil {
		log.Printf("[CONTROL] Failed to build control action")
		return
	}

	ce.executeControl(action)
}

func (ce *ControlExecutor) buildControlAction(alert *models.OscillationAlert) *models.ControlAction {
	now := time.Now()
	actionID := fmt.Sprintf("CTRL-%d-%s", now.UnixNano(), alert.SectionName)

	severity := alert.Severity
	tripRatio := 0.0
	brakeRatio := 0.0

	switch severity {
	case "EMERGENCY":
		tripRatio = 0.25
		brakeRatio = 1.0
	case "ALERT":
		if alert.Diverging {
			tripRatio = 0.15
			brakeRatio = 0.75
		} else {
			tripRatio = 0.1
			brakeRatio = 0.5
		}
	case "WARNING":
		tripRatio = 0.05
		brakeRatio = 0.25
	}

	targetStations := []string{alert.FromStation}

	tripGenerators := []string{}
	totalTripMW := 0.0
	for _, station := range targetStations {
		gens, ok := ce.stationGenerators[station]
		if !ok {
			continue
		}
		cap, ok := ce.tripCapacityMW[station]
		if !ok {
			continue
		}
		genCount := int(float64(len(gens)) * tripRatio)
		if genCount < 1 && tripRatio > 0 {
			genCount = 1
		}
		for i := 0; i < genCount && i < len(gens); i++ {
			tripGenerators = append(tripGenerators, gens[i])
			totalTripMW += cap / float64(len(gens))
		}
	}

	brakingResistors := []string{}
	totalBrakeMW := 0.0
	for _, station := range targetStations {
		brakers, ok := ce.stationBrakers[station]
		if !ok {
			continue
		}
		cap, ok := ce.brakeCapacityMW[station]
		if !ok {
			continue
		}
		brakeCount := int(float64(len(brakers)) * brakeRatio)
		if brakeCount < 1 && brakeRatio > 0 {
			brakeCount = 1
		}
		for i := 0; i < brakeCount && i < len(brakers); i++ {
			brakingResistors = append(brakingResistors, brakers[i])
			totalBrakeMW += cap / float64(len(brakers))
		}
	}

	return &models.ControlAction{
		Timestamp:        now,
		UnixNano:         now.UnixNano(),
		ActionID:         actionID,
		Priority:         1,
		ActionType:       "TRIP_AND_BRAKE",
		TargetStations:   targetStations,
		TripGenerators:   tripGenerators,
		BrakingResistors: brakingResistors,
		TripAmountMW:     totalTripMW,
		BrakeAmountMW:    totalBrakeMW,
		TriggeredBy:      fmt.Sprintf("PronyAnalysis-%s", alert.AlertType),
		AlertRef:         alert,
		Executed:         false,
		ExecutionTimeMs:  0,
	}
}

func (ce *ControlExecutor) executeControl(action *models.ControlAction) {
	startTime := time.Now()

	ce.mu.Lock()
	ce.lastActionTime = startTime
	ce.mu.Unlock()

	ce.registerAction()

	log.Printf("================================================")
	log.Printf("[CONTROL:%s] EXECUTING HIGHEST PRIORITY CONTROL ACTION", action.ActionID)
	log.Printf("  Target Stations:    %v", action.TargetStations)
	log.Printf("  Generators to Trip: %v (%.1f MW total)", action.TripGenerators, action.TripAmountMW)
	log.Printf("  Braking Resistors:  %v (%.1f MW total)", action.BrakingResistors, action.BrakeAmountMW)
	log.Printf("  Trigger:            %s", action.TriggeredBy)
	if action.AlertRef != nil {
		log.Printf("  Dominant Mode:      f=%.3fHz, ζ=%.4f, gradient=%.6f",
			action.AlertRef.DominantMode.Frequency,
			action.AlertRef.DominantMode.DampingRatio,
			action.AlertRef.DampingGradient)
	}
	log.Printf("================================================")

	action.Executed = true
	action.ExecutionTimeMs = time.Since(startTime).Milliseconds()

	ce.mu.Lock()
	ce.executedActions[action.ActionID] = action
	ce.mu.Unlock()

	select {
	case ce.controlChan <- action:
	default:
		log.Printf("[CONTROL] Control channel full, action logged but not sent via channel")
	}

	if ce.broadcastFunc != nil {
		ce.broadcastFunc(action)
	}

	log.Printf("[CONTROL:%s] Action completed in %d ms", action.ActionID, action.ExecutionTimeMs)
}

func (ce *ControlExecutor) canIssueAction() bool {
	ce.windowMu.Lock()
	defer ce.windowMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-1 * time.Hour)

	valid := []time.Time{}
	for _, t := range ce.actionCountWindow {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	ce.actionCountWindow = valid

	if len(ce.actionCountWindow) >= ce.maxActionsPerHour {
		return false
	}
	return true
}

func (ce *ControlExecutor) registerAction() {
	ce.windowMu.Lock()
	defer ce.windowMu.Unlock()
	ce.actionCountWindow = append(ce.actionCountWindow, time.Now())
}

func (ce *ControlExecutor) GetExecutedActions() []*models.ControlAction {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	actions := make([]*models.ControlAction, 0, len(ce.executedActions))
	for _, a := range ce.executedActions {
		actions = append(actions, a)
	}
	return actions
}

func (ce *ControlExecutor) GetStatus() map[string]interface{} {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	ce.windowMu.Lock()
	defer ce.windowMu.Unlock()

	return map[string]interface{}{
		"executedCount":       len(ce.executedActions),
		"actionsThisHour":     len(ce.actionCountWindow),
		"maxActionsPerHour":   ce.maxActionsPerHour,
		"lastActionAgoMs":     time.Since(ce.lastActionTime).Milliseconds(),
		"cooldownRemainingMs": max(0, ce.cooldownPeriod.Milliseconds()-time.Since(ce.lastActionTime).Milliseconds()),
	}
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
