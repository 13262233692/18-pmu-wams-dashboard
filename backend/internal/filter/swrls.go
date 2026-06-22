package filter

import (
	"math"
	"sync"
	"wams-dashboard/internal/models"
)

type SWRLSFilter struct {
	windowSize int
	forgetFactor float64
	mu           sync.RWMutex
	pmuStates    map[string]*pmuFilterState
}

type pmuFilterState struct {
	count       int
	P           [][]float64
	thetaV      [][]float64
	thetaI      [][]float64
	thetaFreq   []float64
	thetaROCOF  []float64
	lastPhaseV  [3]models.ComplexPhasor
	lastPhaseI  [3]models.ComplexPhasor
	lastFreq    float64
	lastROCOF   float64
	lastPosV    models.ComplexPhasor
	lastPosI    models.ComplexPhasor
	initialized bool
}

func NewSWRLSFilter(windowSize int, forgetFactor float64) *SWRLSFilter {
	return &SWRLSFilter{
		windowSize:   windowSize,
		forgetFactor: forgetFactor,
		pmuStates:    make(map[string]*pmuFilterState),
	}
}

func (f *SWRLSFilter) Apply(pm *models.PhasorMeasurement) *models.PhasorMeasurement {
	f.mu.Lock()
	state, exists := f.pmuStates[pm.PMUID]
	if !exists {
		state = f.initState()
		f.pmuStates[pm.PMUID] = state
	}
	f.mu.Unlock()

	result := &models.PhasorMeasurement{
		PMUID:        pm.PMUID,
		IDCode:       pm.IDCode,
		StationName:  pm.StationName,
		Timestamp:    pm.Timestamp,
		UnixNano:     pm.UnixNano,
		SequenceNum:  pm.SequenceNum,
		Status:       pm.Status,
		Filtered:     true,
		ActivePower:  pm.ActivePower,
		ReactivePower: pm.ReactivePower,
		VoltageAngle: pm.VoltageAngle,
	}

	for i := 0; i < 3; i++ {
		result.PhaseVoltage[i] = f.filterPhasor(state, pm.PhaseVoltage[i], i, true)
		result.PhaseCurrent[i] = f.filterPhasor(state, pm.PhaseCurrent[i], i, false)
	}

	result.PositiveSeqV = f.filterComplex(state, pm.PositiveSeqV, true)
	result.PositiveSeqI = f.filterComplex(state, pm.PositiveSeqI, false)

	result.Frequency = f.filterScalar(state, pm.Frequency, true)
	result.ROCOF = f.filterScalar(state, pm.ROCOF, false)

	if !state.initialized {
		state.initialized = true
		state.lastPhaseV = result.PhaseVoltage
		state.lastPhaseI = result.PhaseCurrent
		state.lastFreq = result.Frequency
		state.lastROCOF = result.ROCOF
		state.lastPosV = result.PositiveSeqV
		state.lastPosI = result.PositiveSeqI
	}

	return result
}

func (f *SWRLSFilter) initState() *pmuFilterState {
	n := 3
	P := make([][]float64, n)
	for i := range P {
		P[i] = make([]float64, n)
		P[i][i] = 1000.0
	}

	thetaV := make([][]float64, 3)
	for i := range thetaV {
		thetaV[i] = make([]float64, n)
	}

	thetaI := make([][]float64, 3)
	for i := range thetaI {
		thetaI[i] = make([]float64, n)
	}

	return &pmuFilterState{
		count:      0,
		P:          P,
		thetaV:     thetaV,
		thetaI:     thetaI,
		thetaFreq:  make([]float64, n),
		thetaROCOF: make([]float64, n),
		lastFreq:   50.0,
	}
}

func (f *SWRLSFilter) filterPhasor(state *pmuFilterState, input models.ComplexPhasor, phaseIdx int, isVoltage bool) models.ComplexPhasor {
	var last models.ComplexPhasor
	if isVoltage {
		last = state.lastPhaseV[phaseIdx]
	} else {
		last = state.lastPhaseI[phaseIdx]
	}

	var theta []float64
	if isVoltage {
		theta = state.thetaV[phaseIdx]
	} else {
		theta = state.thetaI[phaseIdx]
	}

	if !state.initialized {
		if isVoltage {
			state.lastPhaseV[phaseIdx] = input
		} else {
			state.lastPhaseI[phaseIdx] = input
		}
		return input
	}

	lambda := f.forgetFactor

	phi := []float64{1.0, float64(state.count), float64(state.count * state.count)}

	yReal := input.Real
	yImag := input.Imag

	filteredReal := f.rlsStep(state, phi, yReal, theta, lambda)
	filteredImag := f.rlsStep(state, phi, yImag, theta, lambda)

	magnitude := math.Sqrt(filteredReal*filteredReal + filteredImag*filteredImag)
	angle := math.Atan2(filteredImag, filteredReal)

	if !isValidMeasurement(input, last) {
		return last
	}

	result := models.ComplexPhasor{
		Real:      filteredReal,
		Imag:      filteredImag,
		Magnitude: magnitude,
		Angle:     angle,
	}

	if isVoltage {
		state.lastPhaseV[phaseIdx] = result
	} else {
		state.lastPhaseI[phaseIdx] = result
	}

	return result
}

func (f *SWRLSFilter) filterComplex(state *pmuFilterState, input models.ComplexPhasor, isVoltage bool) models.ComplexPhasor {
	var last models.ComplexPhasor
	if isVoltage {
		last = state.lastPosV
	} else {
		last = state.lastPosI
	}

	if !state.initialized {
		if isVoltage {
			state.lastPosV = input
		} else {
			state.lastPosI = input
		}
		return input
	}

	lambda := f.forgetFactor
	phi := []float64{1.0, float64(state.count), float64(state.count * state.count)}

	theta := make([]float64, 3)
	filteredReal := f.rlsStep(state, phi, input.Real, theta, lambda)
	filteredImag := f.rlsStep(state, phi, input.Imag, theta, lambda)

	magnitude := math.Sqrt(filteredReal*filteredReal + filteredImag*filteredImag)
	angle := math.Atan2(filteredImag, filteredReal)

	if !isValidMeasurement(input, last) {
		return last
	}

	result := models.ComplexPhasor{
		Real:      filteredReal,
		Imag:      filteredImag,
		Magnitude: magnitude,
		Angle:     angle,
	}

	if isVoltage {
		state.lastPosV = result
	} else {
		state.lastPosI = result
	}

	return result
}

func (f *SWRLSFilter) filterScalar(state *pmuFilterState, input float64, isFreq bool) float64 {
	var last float64
	var theta []float64
	if isFreq {
		last = state.lastFreq
		theta = state.thetaFreq
	} else {
		last = state.lastROCOF
		theta = state.thetaROCOF
	}

	if !state.initialized {
		if isFreq {
			state.lastFreq = input
		} else {
			state.lastROCOF = input
		}
		return input
	}

	if math.Abs(input-last) > 5.0 && isFreq {
		return last
	}

	lambda := f.forgetFactor
	phi := []float64{1.0, float64(state.count), float64(state.count * state.count)}

	filtered := f.rlsStep(state, phi, input, theta, lambda)

	if isFreq {
		state.lastFreq = filtered
	} else {
		state.lastROCOF = filtered
	}

	return filtered
}

func (f *SWRLSFilter) rlsStep(state *pmuFilterState, phi []float64, y float64, theta []float64, lambda float64) float64 {
	n := len(phi)

	yPred := 0.0
	for i := 0; i < n; i++ {
		yPred += theta[i] * phi[i]
	}

	e := y - yPred

	Pphi := make([]float64, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			Pphi[i] += state.P[i][j] * phi[j]
		}
	}

	phiTPphi := 0.0
	for i := 0; i < n; i++ {
		phiTPphi += phi[i] * Pphi[i]
	}

	denominator := lambda + phiTPphi
	if math.Abs(denominator) < 1e-10 {
		state.count++
		return yPred
	}

	K := make([]float64, n)
	for i := 0; i < n; i++ {
		K[i] = Pphi[i] / denominator
	}

	for i := 0; i < n; i++ {
		theta[i] += K[i] * e
	}

	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			state.P[i][j] = (state.P[i][j] - K[i]*Pphi[j]) / lambda
		}
	}

	state.count++
	if state.count > f.windowSize {
		state.count = 0
	}

	yPred = 0.0
	for i := 0; i < n; i++ {
		yPred += theta[i] * phi[i]
	}

	return yPred
}

func isValidMeasurement(current, last models.ComplexPhasor) bool {
	magDiff := math.Abs(current.Magnitude - last.Magnitude)
	if last.Magnitude > 0 {
		ratio := magDiff / last.Magnitude
		if ratio > 0.5 {
			return false
		}
	}

	angleDiff := math.Abs(current.Angle - last.Angle)
	for angleDiff > math.Pi {
		angleDiff -= 2 * math.Pi
	}
	for angleDiff < -math.Pi {
		angleDiff += 2 * math.Pi
	}
	if math.Abs(angleDiff) > math.Pi/2 {
		return false
	}

	return true
}
