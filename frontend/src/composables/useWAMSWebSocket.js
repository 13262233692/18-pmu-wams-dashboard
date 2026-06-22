import { ref, reactive } from 'vue'

const MAX_ANGLE_DIFF_POINTS = 1000
const MAX_ANGLE_CHANGE = 30
const MAX_FREQ_CHANGE = 0.5
const MAX_VOLTAGE_CHANGE = 0.1
const SLIDING_WINDOW_SIZE = 10

const dataQuality = reactive({
  totalMessages: 0,
  invalidMessages: 0,
  spikeMessages: 0,
  lastInvalidTime: null
})

const lastValidValues = reactive({})
const slidingWindows = reactive({})

function isValidNumber(value) {
  return typeof value === 'number' &&
    !isNaN(value) &&
    isFinite(value)
}

function validatePhasor(pmu) {
  if (!pmu || !pmu.pmuId) return false

  const fields = [
    'positiveSeqVoltageMag', 'positiveSeqVoltageAng',
    'positiveSeqCurrentMag', 'positiveSeqCurrentAng',
    'frequency', 'rocof'
  ]

  for (const field of fields) {
    if (pmu[field] !== undefined && !isValidNumber(pmu[field])) {
      return false
    }
  }

  if (pmu.positiveSeqVoltageMag < 0 || pmu.positiveSeqVoltageMag > 1000) {
    return false
  }

  if (pmu.positiveSeqVoltageAng < -180 || pmu.positiveSeqVoltageAng > 180) {
    return false
  }

  if (pmu.frequency < 45 || pmu.frequency > 55) {
    return false
  }

  return true
}

function detectSpike(pmu) {
  const pmuId = pmu.pmuId
  const last = lastValidValues[pmuId]

  if (!last) {
    lastValidValues[pmuId] = { ...pmu }
    return false
  }

  const angleDiff = Math.abs(pmu.positiveSeqVoltageAng - last.positiveSeqVoltageAng)
  const freqDiff = Math.abs(pmu.frequency - last.frequency)
  const voltageDiff = Math.abs(pmu.positiveSeqVoltageMag - last.positiveSeqVoltageMag) / (last.positiveSeqVoltageMag || 1)

  if (angleDiff > MAX_ANGLE_CHANGE ||
      freqDiff > MAX_FREQ_CHANGE ||
      voltageDiff > MAX_VOLTAGE_CHANGE) {
    return true
  }

  lastValidValues[pmuId] = { ...pmu }
  return false
}

function smoothWithSlidingWindow(pmu) {
  const pmuId = pmu.pmuId

  if (!slidingWindows[pmuId]) {
    slidingWindows[pmuId] = []
  }

  const window = slidingWindows[pmuId]
  window.push({ ...pmu })

  if (window.length > SLIDING_WINDOW_SIZE) {
    window.shift()
  }

  if (window.length < 3) {
    return pmu
  }

  const smoothed = { ...pmu }

  smoothed.positiveSeqVoltageMag = window.reduce((sum, p) => sum + p.positiveSeqVoltageMag, 0) / window.length
  smoothed.positiveSeqVoltageAng = window.reduce((sum, p) => sum + p.positiveSeqVoltageAng, 0) / window.length
  smoothed.positiveSeqCurrentMag = window.reduce((sum, p) => sum + p.positiveSeqCurrentMag, 0) / window.length
  smoothed.positiveSeqCurrentAng = window.reduce((sum, p) => sum + p.positiveSeqCurrentAng, 0) / window.length
  smoothed.frequency = window.reduce((sum, p) => sum + p.frequency, 0) / window.length
  smoothed.rocof = window.reduce((sum, p) => sum + p.rocof, 0) / window.length

  return smoothed
}

function validateAngleDiff(diff) {
  if (!diff || !diff.sectionName) return false

  if (!isValidNumber(diff.angleDifference)) return false

  if (diff.angleDifference < -180 || diff.angleDifference > 180) {
    return false
  }

  if (Math.abs(diff.angleDifference) > 90) {
    dataQuality.spikeMessages++
    return false
  }

  return true
}

export function useWAMSWebSocket() {
  const isConnected = ref(false)
  const ws = ref(null)
  const reconnectTimer = ref(null)
  const reconnectAttempts = ref(0)

  const pmuStates = reactive({})
  const angleDiffHistory = ref([])
  const oscillationAlerts = ref([])
  const controlActions = ref([])
  const activeAlerts = reactive({})

  const oscillationCallbacks = []
  const controlCallbacks = []

  function onOscillationAlert(callback) {
    oscillationCallbacks.push(callback)
  }

  function onControlAction(callback) {
    controlCallbacks.push(callback)
  }

  const processMessage = (data) => {
    try {
      dataQuality.totalMessages++

      const msg = JSON.parse(data)

      if (msg.type === 'phasor' && msg.data) {
        const pmu = msg.data

        if (!validatePhasor(pmu)) {
          dataQuality.invalidMessages++
          dataQuality.lastInvalidTime = Date.now()
          return
        }

        if (detectSpike(pmu)) {
          dataQuality.spikeMessages++
          return
        }

        const smoothedPmu = smoothWithSlidingWindow(pmu)
        pmuStates[pmu.pmuId] = smoothedPmu
      }

      if (msg.type === 'angleDiff' && msg.angleDiff) {
        const diff = msg.angleDiff

        if (!validateAngleDiff(diff)) {
          dataQuality.invalidMessages++
          dataQuality.lastInvalidTime = Date.now()
          return
        }

        angleDiffHistory.value.push(diff)

        if (angleDiffHistory.value.length > MAX_ANGLE_DIFF_POINTS) {
          const sectionCounts = {}
          angleDiffHistory.value.forEach(item => {
            sectionCounts[item.sectionName] = (sectionCounts[item.sectionName] || 0) + 1
          })

          const targetSize = MAX_ANGLE_DIFF_POINTS - 100
          const trimmed = []
          const sectionAdded = {}

          for (let i = angleDiffHistory.value.length - 1; i >= 0 && trimmed.length < targetSize; i--) {
            const item = angleDiffHistory.value[i]
            const section = item.sectionName
            if (!sectionAdded[section]) {
              sectionAdded[section] = 0
            }
            if (sectionAdded[section] < 200) {
              trimmed.unshift(item)
              sectionAdded[section]++
            }
          }
          angleDiffHistory.value = trimmed
        }
      }

      if (msg.type === 'oscAlert' && msg.oscAlert) {
        const alert = msg.oscAlert
        oscillationAlerts.value.push(alert)
        if (oscillationAlerts.value.length > 100) {
          oscillationAlerts.value = oscillationAlerts.value.slice(-100)
        }
        activeAlerts[alert.sectionName] = alert

        try {
          oscillationCallbacks.forEach(cb => cb(alert))
        } catch (e) {
          console.error('Error in oscillation callback:', e)
        }

        console.warn('[OSC-ALERT]', alert.severity, alert.sectionName,
          'f=' + alert.dominantMode.frequency.toFixed(3) + 'Hz',
          'ζ=' + alert.dominantMode.dampingRatio.toFixed(4),
          'conf=' + (alert.confidenceLevel * 100).toFixed(0) + '%')
      }

      if (msg.type === 'controlAction' && msg.controlAction) {
        const action = msg.controlAction
        controlActions.value.push(action)
        if (controlActions.value.length > 50) {
          controlActions.value = controlActions.value.slice(-50)
        }

        try {
          controlCallbacks.forEach(cb => cb(action))
        } catch (e) {
          console.error('Error in control callback:', e)
        }

        console.error('[CONTROL-ACTION]', action.actionType,
          'trip=' + action.tripAmountMW.toFixed(0) + 'MW',
          'brake=' + action.brakeAmountMW.toFixed(0) + 'MW',
          'targets=' + action.targetStations.join(','))
      }
    } catch (e) {
      dataQuality.invalidMessages++
      dataQuality.lastInvalidTime = Date.now()
      console.error('Failed to parse WebSocket message:', e)
    }
  }

  const connect = () => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/ws`

    try {
      ws.value = new WebSocket(wsUrl)

      ws.value.onopen = () => {
        console.log('WebSocket connected')
        isConnected.value = true
        reconnectAttempts.value = 0
      }

      ws.value.onmessage = (event) => {
        processMessage(event.data)
      }

      ws.value.onclose = () => {
        console.log('WebSocket disconnected')
        isConnected.value = false
        scheduleReconnect()
      }

      ws.value.onerror = (error) => {
        console.error('WebSocket error:', error)
        isConnected.value = false
      }
    } catch (e) {
      console.error('Failed to create WebSocket:', e)
      scheduleReconnect()
    }
  }

  const scheduleReconnect = () => {
    if (reconnectTimer.value) {
      clearTimeout(reconnectTimer.value)
    }

    reconnectAttempts.value++
    const delay = Math.min(1000 * Math.pow(2, reconnectAttempts.value), 10000)

    reconnectTimer.value = setTimeout(() => {
      console.log(`Attempting to reconnect (${reconnectAttempts.value})...`)
      connect()
    }, delay)
  }

  const disconnect = () => {
    if (reconnectTimer.value) {
      clearTimeout(reconnectTimer.value)
      reconnectTimer.value = null
    }

    if (ws.value) {
      ws.value.close()
      ws.value = null
    }

    isConnected.value = false
  }

  return {
    isConnected,
    pmuStates,
    angleDiffHistory,
    oscillationAlerts,
    controlActions,
    activeAlerts,
    dataQuality,
    connect,
    disconnect,
    onOscillationAlert,
    onControlAction
  }
}
