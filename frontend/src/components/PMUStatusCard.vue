<template>
  <div class="pmu-card" :class="{ warning: isWarning }">
    <div class="pmu-header">
      <div class="pmu-name">{{ pmu.stationName || pmu.pmuId }}</div>
      <div class="pmu-status">
        <span class="status-dot"></span>
        <span>{{ pmu.filtered ? '已滤波' : '原始' }}</span>
      </div>
    </div>
    <div class="pmu-body">
      <div class="pmu-row">
        <span class="label">正序电压</span>
        <span class="value">{{ formatMagnitude(pmu.positiveSeqV?.magnitude) }} kV</span>
      </div>
      <div class="pmu-row">
        <span class="label">电压相角</span>
        <span class="value angle" :class="getAngleClass(pmu.positiveSeqV?.angle)">
          {{ formatAngle(pmu.positiveSeqV?.angle) }}°
        </span>
      </div>
      <div class="pmu-row">
        <span class="label">系统频率</span>
        <span class="value" :class="getFreqClass(pmu.frequency)">
          {{ formatFreq(pmu.frequency) }} Hz
        </span>
      </div>
      <div class="pmu-row">
        <span class="label">频率变化率</span>
        <span class="value">
          {{ formatROCOF(pmu.rocof) }} Hz/s
        </span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  pmu: {
    type: Object,
    required: true
  }
})

const isWarning = computed(() => {
  const freq = props.pmu.frequency
  const angle = props.pmu.positiveSeqV?.angle * 180 / Math.PI
  if (freq && (freq < 49.5 || freq > 50.5)) return true
  if (Math.abs(angle) > 45) return true
  return false
})

const formatMagnitude = (val) => {
  if (val === undefined || val === null || isNaN(val)) return '--'
  return val.toFixed(1)
}

const formatAngle = (rad) => {
  if (rad === undefined || rad === null || isNaN(rad)) return '--'
  return (rad * 180 / Math.PI).toFixed(2)
}

const formatFreq = (val) => {
  if (val === undefined || val === null || isNaN(val)) return '--'
  return val.toFixed(3)
}

const formatROCOF = (val) => {
  if (val === undefined || val === null || isNaN(val)) return '--'
  return val.toFixed(3)
}

const getAngleClass = (rad) => {
  if (rad === undefined || rad === null) return ''
  const deg = rad * 180 / Math.PI
  if (Math.abs(deg) > 30) return 'danger'
  if (Math.abs(deg) > 15) return 'warning'
  return 'normal'
}

const getFreqClass = (freq) => {
  if (!freq) return ''
  if (Math.abs(freq - 50) > 0.2) return 'danger'
  if (Math.abs(freq - 50) > 0.05) return 'warning'
  return 'normal'
}
</script>

<style scoped>
.pmu-card {
  background: linear-gradient(135deg, rgba(30, 45, 75, 0.6) 0%, rgba(20, 35, 60, 0.8) 100%);
  border-radius: 10px;
  padding: 14px;
  border: 1px solid rgba(100, 150, 255, 0.15);
  transition: all 0.3s;
}

.pmu-card:hover {
  border-color: rgba(100, 150, 255, 0.4);
  transform: translateY(-2px);
  box-shadow: 0 8px 24px rgba(0, 100, 255, 0.15);
}

.pmu-card.warning {
  border-color: rgba(255, 180, 0, 0.5);
  background: linear-gradient(135deg, rgba(75, 55, 20, 0.6) 0%, rgba(55, 40, 15, 0.8) 100%);
}

.pmu-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  padding-bottom: 10px;
  border-bottom: 1px solid rgba(100, 150, 255, 0.1);
}

.pmu-name {
  font-size: 14px;
  font-weight: 600;
  color: #c0d0ff;
  letter-spacing: 0.5px;
}

.pmu-status {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: #7aa5e0;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #00dc82;
  animation: pulse 2s infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

.pmu-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.pmu-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.label {
  font-size: 11px;
  color: #7aa5e0;
}

.value {
  font-size: 13px;
  font-weight: 600;
  color: #4facfe;
  font-family: 'Consolas', monospace;
}

.value.normal {
  color: #00dc82;
}

.value.warning {
  color: #feca57;
}

.value.danger {
  color: #ff6b6b;
}

.value.angle {
  color: #00f2fe;
}
</style>
