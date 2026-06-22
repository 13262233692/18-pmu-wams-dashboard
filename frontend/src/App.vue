<template>
  <div class="dashboard-container">
    <header class="dashboard-header">
      <div class="header-left">
        <div class="logo">⚡</div>
        <div class="title-section">
          <h1>超高压电网广域测量系统 (WAMS)</h1>
          <p class="subtitle">省调调度中心 · 态势感知中枢</p>
        </div>
      </div>
      <div class="header-right">
        <div class="status-indicator" :class="{ connected: isConnected }">
          <span class="status-dot"></span>
          <span>{{ isConnected ? '数据流正常' : '连接中断' }}</span>
        </div>
        <div class="system-time">
          <span class="time-label">系统时间</span>
          <span class="time-value">{{ currentTime }}</span>
        </div>
      </div>
    </header>

    <main class="dashboard-main">
      <section class="section angle-diff-section">
        <div class="section-header">
          <h2>省际输电断面相角差实时监测</h2>
          <div class="section-stats">
            <div class="stat-item">
              <span class="stat-label">监测断面</span>
              <span class="stat-value">{{ monitoredSections }}</span>
            </div>
            <div class="stat-item">
              <span class="stat-label">数据点</span>
              <span class="stat-value">{{ totalDataPoints }}</span>
            </div>
          </div>
        </div>
        <AngleDiffChart :angleDiffHistory="angleDiffHistory" />
      </section>

      <section class="section pmu-status-section">
        <div class="section-header">
          <h2>变电站 PMU 实时状态</h2>
        </div>
        <div class="pmu-grid">
          <PMUStatusCard
            v-for="pmu in pmuList"
            :key="pmu.pmuId"
            :pmu="pmu"
          />
        </div>
      </section>
    </main>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import AngleDiffChart from './components/AngleDiffChart.vue'
import PMUStatusCard from './components/PMUStatusCard.vue'
import { useWAMSWebSocket } from './composables/useWAMSWebSocket'

const {
  isConnected,
  angleDiffHistory,
  pmuStates,
  connect,
  disconnect
} = useWAMSWebSocket()

const currentTime = ref('')
const timeInterval = ref(null)

const pmuList = computed(() => {
  return Object.values(pmuStates)
})

const monitoredSections = computed(() => {
  const sections = new Set()
  angleDiffHistory.value.forEach(item => {
    sections.add(item.sectionName)
  })
  return sections.size
})

const totalDataPoints = computed(() => {
  return angleDiffHistory.value.length
})

const updateTime = () => {
  const now = new Date()
  currentTime.value = now.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

onMounted(() => {
  updateTime()
  timeInterval.value = setInterval(updateTime, 1000)
  connect()
})

onUnmounted(() => {
  if (timeInterval.value) {
    clearInterval(timeInterval.value)
  }
  disconnect()
})
</script>

<style scoped>
.dashboard-container {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.dashboard-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 32px;
  background: linear-gradient(90deg, rgba(20, 30, 60, 0.95) 0%, rgba(30, 40, 80, 0.9) 100%);
  border-bottom: 1px solid rgba(100, 150, 255, 0.2);
  box-shadow: 0 2px 20px rgba(0, 100, 255, 0.1);
}

.header-left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.logo {
  font-size: 42px;
  filter: drop-shadow(0 0 10px rgba(0, 200, 255, 0.5));
}

.title-section h1 {
  font-size: 22px;
  font-weight: 700;
  background: linear-gradient(90deg, #4facfe 0%, #00f2fe 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  letter-spacing: 2px;
}

.title-section .subtitle {
  font-size: 13px;
  color: #7aa5e0;
  margin-top: 4px;
  letter-spacing: 1px;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 32px;
}

.status-indicator {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  background: rgba(255, 80, 80, 0.15);
  border-radius: 20px;
  border: 1px solid rgba(255, 80, 80, 0.3);
  font-size: 13px;
  transition: all 0.3s;
}

.status-indicator.connected {
  background: rgba(0, 220, 130, 0.15);
  border-color: rgba(0, 220, 130, 0.4);
  color: #5effb4;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #ff5050;
  animation: pulse 2s infinite;
}

.status-indicator.connected .status-dot {
  background: #00dc82;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

.system-time {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
}

.time-label {
  font-size: 11px;
  color: #7aa5e0;
}

.time-value {
  font-size: 15px;
  font-weight: 600;
  color: #c0d0ff;
  font-family: 'Consolas', monospace;
}

.dashboard-main {
  flex: 1;
  display: grid;
  grid-template-rows: 1fr auto;
  gap: 16px;
  padding: 16px 32px;
  overflow: hidden;
}

.section {
  background: rgba(20, 30, 55, 0.6);
  border-radius: 12px;
  border: 1px solid rgba(100, 150, 255, 0.15);
  padding: 20px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.angle-diff-section {
  min-height: 0;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  flex-shrink: 0;
}

.section-header h2 {
  font-size: 18px;
  color: #a0c0ff;
  font-weight: 600;
  letter-spacing: 1px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.section-header h2::before {
  content: '';
  width: 4px;
  height: 18px;
  background: linear-gradient(180deg, #4facfe 0%, #00f2fe 100%);
  border-radius: 2px;
}

.section-stats {
  display: flex;
  gap: 24px;
}

.stat-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 6px 14px;
  background: rgba(80, 120, 200, 0.1);
  border-radius: 8px;
}

.stat-label {
  font-size: 11px;
  color: #7aa5e0;
}

.stat-value {
  font-size: 20px;
  font-weight: 700;
  color: #4facfe;
  font-family: 'Consolas', monospace;
}

.pmu-status-section {
  max-height: 200px;
}

.pmu-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
  overflow-y: auto;
  flex: 1;
  padding-right: 8px;
}

.pmu-grid::-webkit-scrollbar {
  width: 6px;
}

.pmu-grid::-webkit-scrollbar-track {
  background: rgba(255, 255, 255, 0.05);
  border-radius: 3px;
}

.pmu-grid::-webkit-scrollbar-thumb {
  background: rgba(100, 150, 255, 0.3);
  border-radius: 3px;
}
</style>
