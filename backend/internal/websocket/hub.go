package websocket

import (
	"encoding/json"
	"log"
	"math"
	"sync"
	"time"

	fiberws "github.com/gofiber/contrib/websocket"
	"wams-dashboard/internal/models"
)

type Hub struct {
	clients       map[*fiberws.Conn]bool
	broadcast     chan *models.PhasorMeasurement
	register      chan *fiberws.Conn
	unregister    chan *fiberws.Conn
	mu            sync.RWMutex
	pmuStates     map[string]*models.PhasorMeasurement
	sections      []models.TransmissionSection
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*fiberws.Conn]bool),
		broadcast:  make(chan *models.PhasorMeasurement, 10000),
		register:   make(chan *fiberws.Conn),
		unregister: make(chan *fiberws.Conn),
		pmuStates:  make(map[string]*models.PhasorMeasurement),
		sections: []models.TransmissionSection{
			{Name: "华东-华北断面", FromStation: "华东-换流站A", ToStation: "华北-变电站B"},
			{Name: "华中-华东断面", FromStation: "华中-变电站C", ToStation: "华东-换流站A"},
			{Name: "西北-华中断面", FromStation: "西北-变电站D", ToStation: "华中-变电站C"},
			{Name: "西南-华中断面", FromStation: "西南-换流站E", ToStation: "华中-变电站C"},
			{Name: "华南-华东断面", FromStation: "华南-变电站F", ToStation: "华东-换流站A"},
			{Name: "东北-华北断面", FromStation: "东北-变电站G", ToStation: "华北-变电站B"},
		},
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("WebSocket client connected, total: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()
			log.Printf("WebSocket client disconnected, total: %d", len(h.clients))

		case pm := <-h.broadcast:
			h.processAndBroadcast(pm)
		}
	}
}

func (h *Hub) processAndBroadcast(pm *models.PhasorMeasurement) {
	h.mu.Lock()
	h.pmuStates[pm.PMUID] = pm
	h.mu.Unlock()

	h.sendToClients(models.WSMessage{
		Type: "phasor",
		Data: pm,
	})

	h.calculateAndSendAngleDiffs(pm)
}

func (h *Hub) calculateAndSendAngleDiffs(pm *models.PhasorMeasurement) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	angleDiffs := h.calculateAngleDiffs()
	for _, ad := range angleDiffs {
		h.sendToClients(models.WSMessage{
			Type:      "angleDiff",
			AngleDiff: ad,
		})
	}
}

func (h *Hub) calculateAngleDiffs() []*models.AngleDiffData {
	var results []*models.AngleDiffData
	now := time.Now()

	for _, section := range h.sections {
		fromPM, fromOk := h.findPMUByStation(section.FromStation)
		toPM, toOk := h.findPMUByStation(section.ToStation)

		if fromOk && toOk {
			angleDiff := h.calculateAngleDifference(
				fromPM.PositiveSeqV.Angle,
				toPM.PositiveSeqV.Angle,
			)

			details := map[string]float64{
				section.FromStation + "_angle": fromPM.PositiveSeqV.Angle * 180 / math.Pi,
				section.ToStation + "_angle":   toPM.PositiveSeqV.Angle * 180 / math.Pi,
			}

			results = append(results, &models.AngleDiffData{
				Timestamp:   now,
				UnixNano:    now.UnixNano(),
				SectionName: section.Name,
				AngleDiff:   angleDiff,
				Details:     details,
			})
		}
	}

	return results
}

func (h *Hub) findPMUByStation(stationName string) (*models.PhasorMeasurement, bool) {
	for _, pm := range h.pmuStates {
		if pm.StationName == stationName {
			return pm, true
		}
	}
	return nil, false
}

func (h *Hub) calculateAngleDifference(angle1, angle2 float64) float64 {
	diff := angle1 - angle2
	for diff > math.Pi {
		diff -= 2 * math.Pi
	}
	for diff < -math.Pi {
		diff += 2 * math.Pi
	}
	return diff * 180 / math.Pi
}

func (h *Hub) sendToClients(msg models.WSMessage) {
	if msg.Data != nil {
		sanitizePhasor(msg.Data)
	}
	if msg.AngleDiff != nil {
		sanitizeAngleDiff(msg.AngleDiff)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("JSON marshal error: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		err := client.WriteMessage(fiberws.TextMessage, data)
		if err != nil {
			go func(c *fiberws.Conn) {
				h.unregister <- c
			}(client)
		}
	}
}

func (h *Hub) Broadcast(pm *models.PhasorMeasurement) {
	select {
	case h.broadcast <- pm:
	default:
	}
}

func (h *Hub) HandleConnection(c *fiberws.Conn) {
	defer func() {
		h.unregister <- c
	}()

	h.register <- c

	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (h *Hub) GetSections() []models.TransmissionSection {
	return h.sections
}

func sanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0.0
	}
	return v
}

func sanitizePhasor(pm *models.PhasorMeasurement) {
	for i := range pm.PhaseVoltage {
		pm.PhaseVoltage[i].Magnitude = sanitizeFloat(pm.PhaseVoltage[i].Magnitude)
		pm.PhaseVoltage[i].Angle = sanitizeFloat(pm.PhaseVoltage[i].Angle)
		pm.PhaseVoltage[i].Real = sanitizeFloat(pm.PhaseVoltage[i].Real)
		pm.PhaseVoltage[i].Imag = sanitizeFloat(pm.PhaseVoltage[i].Imag)
	}
	for i := range pm.PhaseCurrent {
		pm.PhaseCurrent[i].Magnitude = sanitizeFloat(pm.PhaseCurrent[i].Magnitude)
		pm.PhaseCurrent[i].Angle = sanitizeFloat(pm.PhaseCurrent[i].Angle)
		pm.PhaseCurrent[i].Real = sanitizeFloat(pm.PhaseCurrent[i].Real)
		pm.PhaseCurrent[i].Imag = sanitizeFloat(pm.PhaseCurrent[i].Imag)
	}
	pm.PositiveSeqV.Magnitude = sanitizeFloat(pm.PositiveSeqV.Magnitude)
	pm.PositiveSeqV.Angle = sanitizeFloat(pm.PositiveSeqV.Angle)
	pm.PositiveSeqV.Real = sanitizeFloat(pm.PositiveSeqV.Real)
	pm.PositiveSeqV.Imag = sanitizeFloat(pm.PositiveSeqV.Imag)
	pm.PositiveSeqI.Magnitude = sanitizeFloat(pm.PositiveSeqI.Magnitude)
	pm.PositiveSeqI.Angle = sanitizeFloat(pm.PositiveSeqI.Angle)
	pm.PositiveSeqI.Real = sanitizeFloat(pm.PositiveSeqI.Real)
	pm.PositiveSeqI.Imag = sanitizeFloat(pm.PositiveSeqI.Imag)
	pm.Frequency = sanitizeFloat(pm.Frequency)
	pm.ROCOF = sanitizeFloat(pm.ROCOF)
}

func sanitizeAngleDiff(ad *models.AngleDiffData) {
	ad.AngleDiff = sanitizeFloat(ad.AngleDiff)
	for k, v := range ad.Details {
		ad.Details[k] = sanitizeFloat(v)
	}
}
