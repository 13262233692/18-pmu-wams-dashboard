package detector

import (
	"log"
	"sync"
	"time"

	"wams-dashboard/internal/models"
	"wams-dashboard/internal/prony"
)

type DetectorStatus int

const (
	StatusNormal DetectorStatus = iota
	StatusWarning
	StatusAlert
	StatusEmergency
	StatusControlTriggered
)

func (s DetectorStatus) String() string {
	switch s {
	case StatusNormal:
		return "NORMAL"
	case StatusWarning:
		return "WARNING"
	case StatusAlert:
		return "ALERT"
	case StatusEmergency:
		return "EMERGENCY"
	case StatusControlTriggered:
		return "CONTROL_TRIGGERED"
	default:
		return "UNKNOWN"
	}
}

type SectionDetector struct {
	SectionName   string
	FromStation   string
	ToStation     string

	Analyzer       *prony.PronyAnalyzer
	WindowSize     int
	AnalysisPeriod int
	Fs             float64

	powerBuffer   []float64
	angleBuffer   []float64
	bufferMu      sync.Mutex
	sampleCount   int

	dampingHistory []float64
	divergenceCount int
	negativeCount   int
	lastAlertTime   time.Time

	CurrentStatus  DetectorStatus
	LastResult     *prony.PronyResult
	LastDominant   models.PronyMode
	LastGradient   float64
	LastOscAmp     float64
	Confidence     float64
	AlertCooldown  time.Duration

	alertChan  chan<- *models.OscillationAlert
	statusMu   sync.RWMutex
	runMu      sync.Mutex
	running    bool
	stopChan   chan struct{}
}

func NewSectionDetector(
	sectionName, fromStation, toStation string,
	windowSize int,
	fs float64,
	alertChan chan<- *models.OscillationAlert,
) *SectionDetector {
	order := 10
	if windowSize/6 < order {
		order = windowSize / 6
	}
	if order < 6 {
		order = 6
	}

	return &SectionDetector{
		SectionName:   sectionName,
		FromStation:   fromStation,
		ToStation:     toStation,
		Analyzer:      prony.NewPronyAnalyzer(order, windowSize, fs),
		WindowSize:    windowSize,
		AnalysisPeriod: int(fs / 10),
		Fs:            fs,
		powerBuffer:   make([]float64, 0, windowSize*2),
		angleBuffer:   make([]float64, 0, windowSize*2),
		dampingHistory: make([]float64, 0, 50),
		AlertCooldown:  5 * time.Second,
		alertChan:      alertChan,
		stopChan:       make(chan struct{}),
		CurrentStatus:  StatusNormal,
	}
}

func (sd *SectionDetector) AddSample(sample *models.PowerFlowSample) {
	sd.bufferMu.Lock()
	defer sd.bufferMu.Unlock()

	sd.powerBuffer = append(sd.powerBuffer, sample.ActivePower)
	sd.angleBuffer = append(sd.angleBuffer, sample.AngleDiff)

	if len(sd.powerBuffer) > sd.WindowSize*2 {
		sd.powerBuffer = sd.powerBuffer[len(sd.powerBuffer)-sd.WindowSize*2:]
	}
	if len(sd.angleBuffer) > sd.WindowSize*2 {
		sd.angleBuffer = sd.angleBuffer[len(sd.angleBuffer)-sd.WindowSize*2:]
	}

	sd.sampleCount++
}

func (sd *SectionDetector) shouldAnalyze() bool {
	sd.bufferMu.Lock()
	count := sd.sampleCount
	sd.bufferMu.Unlock()
	if count > 0 && count%50 == 0 {
		log.Printf("[OSC-SHOULD:%s] sampleCount=%d, AnalysisPeriod=%d, result=%v",
			sd.SectionName, count, sd.AnalysisPeriod, count >= sd.AnalysisPeriod)
	}
	return count >= sd.AnalysisPeriod
}

func (sd *SectionDetector) Analyze() {
	sd.runMu.Lock()
	if !sd.shouldAnalyze() {
		sd.runMu.Unlock()
		return
	}

	sd.bufferMu.Lock()
	sampleCount := sd.sampleCount
	powerBufLen := len(sd.powerBuffer)

	log.Printf("[OSC-ANALYZE:%s] sampleCount=%d, powerBufLen=%d, windowSize=%d",
		sd.SectionName, sampleCount, powerBufLen, sd.WindowSize)

	if len(sd.powerBuffer) < sd.WindowSize {
		sd.bufferMu.Unlock()
		sd.runMu.Unlock()
		return
	}

	sd.sampleCount = 0

	powerSig := make([]float64, sd.WindowSize)
	angleSig := make([]float64, sd.WindowSize)
	copy(powerSig, sd.powerBuffer[len(sd.powerBuffer)-sd.WindowSize:])
	copy(angleSig, sd.angleBuffer[len(sd.angleBuffer)-sd.WindowSize:])
	sd.bufferMu.Unlock()

	result, err := sd.Analyzer.Analyze(powerSig)
	if err != nil {
		log.Printf("[OSC-DETECT:%s] Prony analysis error: %v", sd.SectionName, err)
		sd.runMu.Unlock()
		return
	}

	angleResult, _ := sd.Analyzer.Analyze(angleSig)

	sd.LastResult = result
	sd.LastOscAmp = prony.ComputeOscillationAmplitude(powerSig)

	sd.statusMu.Lock()
	defer sd.statusMu.Unlock()
	defer sd.runMu.Unlock()

	sd.evaluateDetection(result, angleResult, powerSig, angleSig)
}

func (sd *SectionDetector) evaluateDetection(
	powerResult *prony.PronyResult,
	angleResult *prony.PronyResult,
	powerSig, angleSig []float64,
) {
	var dominant models.PronyMode
	foundNegative := false
	badnessScore := 0.0

	if len(powerResult.Modes) > 0 {
		for _, m := range powerResult.Modes {
			if m.DampingRatio < 0 && m.EnergyRatio > 0.1 {
				foundNegative = true
				if m.EnergyRatio > dominant.EnergyRatio {
					dominant = m
				}
				badnessScore += (-m.DampingRatio) * m.EnergyRatio
			}
		}
	}

	if dominant.EnergyRatio == 0 && len(powerResult.Modes) > 0 {
		for _, m := range powerResult.Modes {
			if m.EnergyRatio > dominant.EnergyRatio {
				dominant = m
			}
		}
	}

	if dominant.EnergyRatio == 0 {
		if len(angleResult.Modes) > 0 {
			for _, m := range angleResult.Modes {
				if m.DampingRatio < 0 && m.EnergyRatio > 0.1 {
					foundNegative = true
					if m.EnergyRatio > dominant.EnergyRatio {
						dominant = m
					}
					badnessScore += (-m.DampingRatio) * m.EnergyRatio
				}
			}
		}
	}

	sd.LastDominant = dominant

	sd.dampingHistory = append(sd.dampingHistory, dominant.DampingRatio)
	if len(sd.dampingHistory) > 30 {
		sd.dampingHistory = sd.dampingHistory[len(sd.dampingHistory)-30:]
	}

	gradient := 0.0
	if len(sd.dampingHistory) >= 8 {
		gradient = prony.ComputeDampingGradient(sd.dampingHistory, 8)
	}
	sd.LastGradient = gradient

	if foundNegative {
		sd.negativeCount++
		if dominant.DampingRatio < 0 {
			sd.divergenceCount++
		}
	} else {
		if sd.negativeCount > 0 {
			sd.negativeCount--
		}
		if sd.divergenceCount > 0 {
			sd.divergenceCount--
		}
	}

	confidence := 0.0
	if dominant.Frequency >= 0.2 && dominant.Frequency <= 2.5 {
		confidence = dominant.EnergyRatio
		if powerResult.Residual < 0.3 {
			confidence *= (1.0 - powerResult.Residual)
		} else {
			confidence *= 0.5
		}
	}
	sd.Confidence = confidence

	angleSeparation := 0.0
	if len(angleSig) > 0 {
		angleSeparation = prony.ComputeOscillationAmplitude(angleSig) * 2.0
	}

	newStatus := StatusNormal
	switch {
	case sd.negativeCount >= 2 && dominant.DampingRatio < 0 && confidence > 0.1:
		if sd.divergenceCount >= 1 && gradient < 0 {
			newStatus = StatusEmergency
		} else {
			newStatus = StatusAlert
		}
	case sd.negativeCount >= 1 && dominant.DampingRatio < -0.005:
		newStatus = StatusWarning
	case foundNegative && confidence > 0.05:
		newStatus = StatusWarning
	}

	sd.CurrentStatus = newStatus

	if dominant.Frequency > 0 && dominant.Frequency < 5 {
		log.Printf("[OSC-DEBUG:%s] modes=%d, negCount=%d, divCount=%d, f=%.3fHz, ζ=%.4f, conf=%.3f, grad=%.5f, status=%d",
			sd.SectionName, len(powerResult.Modes), sd.negativeCount, sd.divergenceCount,
			dominant.Frequency, dominant.DampingRatio, confidence, gradient, newStatus)
	}

	timeSinceLastAlert := time.Since(sd.lastAlertTime)
	isFirstAlert := sd.lastAlertTime.IsZero()
	cooldownOK := isFirstAlert || timeSinceLastAlert >= sd.AlertCooldown
	log.Printf("[OSC-ALERT-CHECK:%s] newStatus=%d, StatusAlert=%d, isFirstAlert=%v, timeSinceLast=%v, cooldown=%v, cond1=%v, cond2=%v",
		sd.SectionName, newStatus, StatusAlert, isFirstAlert, timeSinceLastAlert, sd.AlertCooldown,
		newStatus >= StatusAlert, cooldownOK)

	if newStatus >= StatusAlert && cooldownOK {
		log.Printf("[OSC-ALERT:%s] Triggering %s alert, f=%.3fHz, ζ=%.4f",
			sd.SectionName, newStatus.String(), dominant.Frequency, dominant.DampingRatio)
		sd.triggerAlert(dominant, powerSig, angleSeparation, newStatus, badnessScore)
		sd.lastAlertTime = time.Now()
	}
}

func (sd *SectionDetector) triggerAlert(
	dominant models.PronyMode,
	powerSig []float64,
	angleSeparation float64,
	status DetectorStatus,
	badnessScore float64,
) {
	now := time.Now()

	severity := "WARNING"
	alertType := "LOW_FREQ_OSC_DETECTED"
	recommendedAction := "Increase PSS gain or reduce power transfer"

	switch status {
	case StatusAlert:
		severity = "ALERT"
		alertType = "NEGATIVE_DAMPING_OSCILLATION"
		recommendedAction = "Consider generator tripping or dynamic braking"
	case StatusEmergency:
		severity = "EMERGENCY"
		alertType = "DIVERGING_OSCILLATION_IMMINENT"
		recommendedAction = "IMMEDIATE: Trip sending-end generators + energize braking resistors"
	}

	if len(powerSig) > 0 {
		lastPower := powerSig[len(powerSig)-1]
		_ = lastPower
	}

	avgPower := 0.0
	if len(powerSig) > 0 {
		for _, p := range powerSig {
			avgPower += p
		}
		avgPower /= float64(len(powerSig))
	}

	alert := &models.OscillationAlert{
		Timestamp:          now,
		UnixNano:           now.UnixNano(),
		SectionName:        sd.SectionName,
		FromStation:        sd.FromStation,
		ToStation:          sd.ToStation,
		Severity:           severity,
		AlertType:          alertType,
		DetectedModes:      append([]models.PronyMode{}, sd.LastResult.Modes...),
		DominantMode:       dominant,
		NegativeDamping:    dominant.DampingRatio < 0,
		Diverging:          sd.divergenceCount >= 3,
		DampingGradient:    sd.LastGradient,
		DivergenceCount:    sd.divergenceCount,
		ActivePowerMW:      avgPower,
		PowerOscAmplitude:  sd.LastOscAmp,
		AngleSeparationDeg: angleSeparation,
		ConfidenceLevel:    sd.Confidence,
		RecommendedAction:  recommendedAction,
	}

	log.Printf("[OSC-ALERT:%s] %s: f=%.3fHz, ζ=%.4f, gradient=%.6f, conf=%.2f%%, div_count=%d",
		sd.SectionName, severity,
		dominant.Frequency, dominant.DampingRatio,
		sd.LastGradient, sd.Confidence*100,
		sd.divergenceCount,
	)

	select {
	case sd.alertChan <- alert:
	default:
		log.Printf("[OSC-ALERT:%s] Alert channel full, dropping alert", sd.SectionName)
	}
}

func (sd *SectionDetector) GetStatus() DetectorStatus {
	sd.statusMu.RLock()
	defer sd.statusMu.RUnlock()
	return sd.CurrentStatus
}

func (sd *SectionDetector) GetSnapshot() map[string]interface{} {
	sd.statusMu.RLock()
	defer sd.statusMu.RUnlock()
	sd.bufferMu.Lock()
	bufLen := len(sd.powerBuffer)
	sd.bufferMu.Unlock()

	return map[string]interface{}{
		"section":       sd.SectionName,
		"status":        sd.CurrentStatus,
		"bufferLength":  bufLen,
		"dominantMode":  sd.LastDominant,
		"dampingGrad":   sd.LastGradient,
		"oscAmplitude":  sd.LastOscAmp,
		"confidence":    sd.Confidence,
		"divCount":      sd.divergenceCount,
		"negCount":      sd.negativeCount,
	}
}

type LowFreqOscillationSystem struct {
	detectors  map[string]*SectionDetector
	detectorMu sync.RWMutex

	alertChan      chan *models.OscillationAlert
	controlChan    chan *models.ControlAction
	sections       []models.TransmissionSection
	pmuSectionMap  map[string][]string

	running    bool
	runMu      sync.Mutex
	stopChan   chan struct{}

	pmuAngleCache map[string]*models.PhasorMeasurement
	cacheMu       sync.RWMutex
}

func NewLowFreqOscillationSystem(
	sections []models.TransmissionSection,
	alertChan chan *models.OscillationAlert,
	controlChan chan *models.ControlAction,
) *LowFreqOscillationSystem {
	system := &LowFreqOscillationSystem{
		detectors:     make(map[string]*SectionDetector),
		alertChan:     alertChan,
		controlChan:   controlChan,
		sections:      sections,
		pmuSectionMap: make(map[string][]string),
		pmuAngleCache: make(map[string]*models.PhasorMeasurement),
		stopChan:      make(chan struct{}),
	}

	windowSize := 100
	fs := 50.0

	for _, sec := range sections {
		det := NewSectionDetector(sec.Name, sec.FromStation, sec.ToStation, windowSize, fs, alertChan)
		system.detectors[sec.Name] = det

		system.pmuSectionMap[sec.FromStation] = append(system.pmuSectionMap[sec.FromStation], sec.Name)
		system.pmuSectionMap[sec.ToStation] = append(system.pmuSectionMap[sec.ToStation], sec.Name)
	}

	return system
}

func (sys *LowFreqOscillationSystem) IngestPMU(pm *models.PhasorMeasurement) {
	sys.cacheMu.Lock()
	sys.pmuAngleCache[pm.StationName] = pm
	sys.cacheMu.Unlock()

	secNames, ok := sys.pmuSectionMap[pm.StationName]
	if !ok {
		log.Printf("[OSC-INGEST] Station %s not found in section map, available stations: %v",
			pm.StationName, sys.getStationNames())
		return
	}

	log.Printf("[OSC-INGEST] Station=%s, sections=%v, ActivePower=%.2f, VoltageAngle=%.2f",
		pm.StationName, secNames, pm.ActivePower, pm.VoltageAngle)

	sys.detectorMu.RLock()
	defer sys.detectorMu.RUnlock()

	for _, secName := range secNames {
		det, ok := sys.detectors[secName]
		if !ok {
			continue
		}

		fromPMU := sys.getCachedPMU(det.FromStation)
		toPMU := sys.getCachedPMU(det.ToStation)
		if fromPMU == nil || toPMU == nil {
			continue
		}

		angleDiff := fromPMU.VoltageAngle - toPMU.VoltageAngle
		for angleDiff > 180 {
			angleDiff -= 360
		}
		for angleDiff < -180 {
			angleDiff += 360
		}

		avgPower := (fromPMU.ActivePower + toPMU.ActivePower) / 2.0

		sample := &models.PowerFlowSample{
			Timestamp:   pm.Timestamp,
			UnixNano:    pm.UnixNano,
			SectionName: secName,
			ActivePower: avgPower,
			AngleDiff:   angleDiff,
			Frequency:   (fromPMU.Frequency + toPMU.Frequency) / 2.0,
		}

		det.AddSample(sample)
	}
}

func (sys *LowFreqOscillationSystem) getCachedPMU(stationName string) *models.PhasorMeasurement {
	sys.cacheMu.RLock()
	defer sys.cacheMu.RUnlock()
	pm, ok := sys.pmuAngleCache[stationName]
	if !ok {
		return nil
	}
	if time.Since(pm.Timestamp) > 1*time.Second {
		return nil
	}
	return pm
}

func (sys *LowFreqOscillationSystem) RunAnalysisLoop() {
	log.Printf("[OSC-SYS] RunAnalysisLoop called")
	sys.runMu.Lock()
	if sys.running {
		log.Printf("[OSC-SYS] Already running, exiting")
		sys.runMu.Unlock()
		return
	}
	sys.running = true
	sys.runMu.Unlock()

	log.Printf("[OSC-SYS] Analysis loop started, detectors=%d", len(sys.detectors))
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	tickCount := 0
	for {
		select {
		case <-sys.stopChan:
			log.Printf("[OSC-SYS] Analysis loop stopped")
			return
		case <-ticker.C:
			tickCount++
			if tickCount%100 == 0 {
				log.Printf("[OSC-SYS] Analysis tick %d, detectors=%d", tickCount, len(sys.detectors))
			}
			sys.detectorMu.RLock()
			for _, det := range sys.detectors {
				det.Analyze()
			}
			sys.detectorMu.RUnlock()
		}
	}
}

func (sys *LowFreqOscillationSystem) getStationNames() []string {
	names := make([]string, 0, len(sys.pmuSectionMap))
	for name := range sys.pmuSectionMap {
		names = append(names, name)
	}
	return names
}

func (sys *LowFreqOscillationSystem) Stop() {
	close(sys.stopChan)
}

func (sys *LowFreqOscillationSystem) GetAllStatus() map[string]DetectorStatus {
	sys.detectorMu.RLock()
	defer sys.detectorMu.RUnlock()
	result := make(map[string]DetectorStatus)
	for name, det := range sys.detectors {
		result[name] = det.GetStatus()
	}
	return result
}

func (sys *LowFreqOscillationSystem) GetSnapshots() map[string]map[string]interface{} {
	sys.detectorMu.RLock()
	defer sys.detectorMu.RUnlock()
	result := make(map[string]map[string]interface{})
	for name, det := range sys.detectors {
		result[name] = det.GetSnapshot()
	}
	return result
}
