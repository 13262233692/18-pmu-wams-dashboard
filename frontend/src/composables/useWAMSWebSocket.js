import { ref, reactive } from 'vue'

const MAX_ANGLE_DIFF_POINTS = 1000

export function useWAMSWebSocket() {
  const isConnected = ref(false)
  const ws = ref(null)
  const reconnectTimer = ref(null)
  const reconnectAttempts = ref(0)

  const pmuStates = reactive({})
  const angleDiffHistory = ref([])

  const processMessage = (data) => {
    try {
      const msg = JSON.parse(data)

      if (msg.type === 'phasor' && msg.data) {
        const pmu = msg.data
        pmuStates[pmu.pmuId] = pmu
      }

      if (msg.type === 'angleDiff' && msg.angleDiff) {
        const diff = msg.angleDiff
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
    } catch (e) {
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
    connect,
    disconnect
  }
}
