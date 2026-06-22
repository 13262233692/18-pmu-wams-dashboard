package models

import "time"

type ComplexPhasor struct {
	Magnitude float64 `json:"magnitude"`
	Angle     float64 `json:"angle"`
	Real      float64 `json:"real"`
	Imag      float64 `json:"imag"`
}

type PhasorMeasurement struct {
	IDCode         uint16         `json:"idCode"`
	PMUID          string         `json:"pmuId"`
	StationName    string         `json:"stationName"`
	Timestamp      time.Time      `json:"timestamp"`
	UnixNano       int64          `json:"unixNano"`
	SequenceNum    uint32         `json:"sequenceNum"`
	PhaseVoltage   [3]ComplexPhasor `json:"phaseVoltage"`
	PhaseCurrent   [3]ComplexPhasor `json:"phaseCurrent"`
	PositiveSeqV   ComplexPhasor  `json:"positiveSeqV"`
	PositiveSeqI   ComplexPhasor  `json:"positiveSeqI"`
	Frequency      float64        `json:"frequency"`
	ROCOF          float64        `json:"rocof"`
	Status         uint16         `json:"status"`
	Filtered       bool           `json:"filtered"`
	ActivePower    float64        `json:"activePower"`
	ReactivePower  float64        `json:"reactivePower"`
	VoltageAngle   float64        `json:"voltageAngle"`
}

type WSMessage struct {
	Type          string             `json:"type"`
	Data          *PhasorMeasurement `json:"data,omitempty"`
	AngleDiff     *AngleDiffData     `json:"angleDiff,omitempty"`
	OscAlert      *OscillationAlert  `json:"oscAlert,omitempty"`
	ControlAction *ControlAction     `json:"controlAction,omitempty"`
}

type AngleDiffData struct {
	Timestamp   time.Time         `json:"timestamp"`
	UnixNano    int64             `json:"unixNano"`
	SectionName string            `json:"sectionName"`
	AngleDiff   float64           `json:"angleDiff"`
	Details     map[string]float64 `json:"details"`
}

type TransmissionSection struct {
	Name        string
	FromStation string
	ToStation   string
}

type PronyMode struct {
	Frequency     float64 `json:"frequency"`
	DampingRatio  float64 `json:"dampingRatio"`
	DampingFactor float64 `json:"dampingFactor"`
	Amplitude     float64 `json:"amplitude"`
	Phase         float64 `json:"phase"`
	EnergyRatio   float64 `json:"energyRatio"`
}

type OscillationAlert struct {
	Timestamp          time.Time   `json:"timestamp"`
	UnixNano           int64       `json:"unixNano"`
	SectionName        string      `json:"sectionName"`
	FromStation        string      `json:"fromStation"`
	ToStation          string      `json:"toStation"`
	Severity           string      `json:"severity"`
	AlertType          string      `json:"alertType"`
	DetectedModes      []PronyMode `json:"detectedModes"`
	DominantMode       PronyMode   `json:"dominantMode"`
	NegativeDamping    bool        `json:"negativeDamping"`
	Diverging          bool        `json:"diverging"`
	DampingGradient    float64     `json:"dampingGradient"`
	DivergenceCount    int         `json:"divergenceCount"`
	ActivePowerMW      float64     `json:"activePowerMW"`
	PowerOscAmplitude  float64     `json:"powerOscAmplitude"`
	AngleSeparationDeg float64     `json:"angleSeparationDeg"`
	ConfidenceLevel    float64     `json:"confidenceLevel"`
	RecommendedAction  string      `json:"recommendedAction"`
}

type ControlAction struct {
	Timestamp       time.Time         `json:"timestamp"`
	UnixNano        int64             `json:"unixNano"`
	ActionID        string            `json:"actionId"`
	Priority        int               `json:"priority"`
	ActionType      string            `json:"actionType"`
	TargetStations  []string          `json:"targetStations"`
	TripGenerators  []string          `json:"tripGenerators"`
	BrakingResistors []string          `json:"brakingResistors"`
	TripAmountMW    float64           `json:"tripAmountMW"`
	BrakeAmountMW   float64           `json:"brakeAmountMW"`
	TriggeredBy     string            `json:"triggeredBy"`
	AlertRef        *OscillationAlert `json:"alertRef"`
	Executed        bool              `json:"executed"`
	ExecutionTimeMs int64             `json:"executionTimeMs"`
}

type PowerFlowSample struct {
	Timestamp   time.Time
	UnixNano    int64
	SectionName string
	ActivePower float64
	AngleDiff   float64
	Frequency   float64
}

