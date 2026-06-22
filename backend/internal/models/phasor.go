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
}

type WSMessage struct {
	Type    string             `json:"type"`
	Data    *PhasorMeasurement `json:"data,omitempty"`
	AngleDiff *AngleDiffData   `json:"angleDiff,omitempty"`
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
