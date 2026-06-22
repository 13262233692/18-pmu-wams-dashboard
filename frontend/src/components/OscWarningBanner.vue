<template>
  <div class="osc-banner-wrapper" v-if="hasActiveAlerts">
    <div
      class="osc-warning-banner"
      :class="{
        'banner-warning': maxSeverity === 'WARNING',
        'banner-alert': maxSeverity === 'ALERT',
        'banner-emergency': maxSeverity === 'EMERGENCY',
        'pulse-high': maxSeverity === 'EMERGENCY'
      }"
    >
      <div class="banner-icon">
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
          <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
          <line x1="12" y1="9" x2="12" y2="13"/>
          <line x1="12" y1="17" x2="12.01" y2="17"/>
        </svg>
      </div>

      <div class="banner-content">
        <div class="banner-title">
          <span class="severity-badge" :class="'badge-' + maxSeverity">{{ severityText }}</span>
          <span class="section-count">
            {{ alertCount }} 条输电断面 {{ alertCount > 1 ? '均' : '' }}检测到
            <strong class="osc-type">低频振荡发散风险</strong>
          </span>
        </div>

        <div class="alert-sections">
          <div
            v-for="(alert, sec) in criticalAlerts"
            :key="sec"
            class="section-item"
            :class="'sev-' + alert.severity"
          >
            <span class="sec-name">{{ alert.sectionName }}</span>
            <span class="sec-mode">
              f<sub>0</sub>={{ alert.dominantMode.frequency.toFixed(3) }}Hz
            </span>
            <span class="sec-damping" :class="{'neg-damp': alert.dominantMode.dampingRatio < 0}">
              ζ={{ (alert.dominantMode.dampingRatio * 100).toFixed(2) }}%
            </span>
            <span class="sec-gradient" :class="{'neg-grad': alert.dampingGradient < 0}">
              dζ/dt={{ (alert.dampingGradient * 1e5).toFixed(2) }}e-5
            </span>
            <span class="sec-conf">
              置信度={{ (alert.confidenceLevel * 100).toFixed(0) }}%
            </span>
            <span v-if="alert.diverging" class="sec-diverge">⚠发散中</span>
          </div>
        </div>

        <div class="banner-footer">
          <span class="recommend">
            推荐措施：
            <strong>{{ primaryRecommendation }}</strong>
          </span>
          <span class="control-status" v-if="controlExecuted">
            ✓ 已执行控制措施 - {{ lastControlAction }}
          </span>
        </div>
      </div>

      <div class="pulse-overlay"></div>
      <div class="pulse-overlay delay-1"></div>
      <div class="pulse-overlay delay-2"></div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'

const props = defineProps({
  activeAlerts: {
    type: Object,
    required: true
  },
  controlActions: {
    type: Array,
    default: () => []
  }
})

const alertList = computed(() => {
  return Object.values(props.activeAlerts || {}).filter(a => {
    const age = Date.now() - new Date(a.timestamp).getTime()
    return age < 60000
  })
})

const hasActiveAlerts = computed(() => alertList.value.length > 0)
const alertCount = computed(() => alertList.value.length)

const severityOrder = { 'WARNING': 1, 'ALERT': 2, 'EMERGENCY': 3 }
const maxSeverity = computed(() => {
  let max = 0
  let sev = 'WARNING'
  for (const a of alertList.value) {
    const s = severityOrder[a.severity] || 0
    if (s > max) {
      max = s
      sev = a.severity
    }
  }
  return sev
})

const severityText = computed(() => {
  const map = {
    'WARNING': '⚠ 低阻尼预警',
    'ALERT': '🚨 负阻尼告警',
    'EMERGENCY': '💥 紧急：振荡发散'
  }
  return map[maxSeverity.value] || '告警'
})

const criticalAlerts = computed(() => {
  const sorted = [...alertList.value].sort((a, b) =>
    (severityOrder[b.severity] || 0) - (severityOrder[a.severity] || 0)
  )
  return sorted.slice(0, 4)
})

const primaryRecommendation = computed(() => {
  if (alertList.value.length === 0) return ''
  const sorted = [...alertList.value].sort((a, b) =>
    (severityOrder[b.severity] || 0) - (severityOrder[a.severity] || 0)
  )
  return sorted[0].recommendedAction || '密切监控'
})

const controlExecuted = ref(false)
const lastControlAction = ref('')

watch(
  () => props.controlActions?.length,
  (newLen) => {
    if (newLen > 0 && props.controlActions) {
      const last = props.controlActions[newLen - 1]
      if (last && last.executed) {
        controlExecuted.value = true
        lastControlAction.value =
          `切机 ${last.tripAmountMW.toFixed(0)}MW + 制动 ${last.brakeAmountMW.toFixed(0)}MW`
      }
    }
  }
)
</script>

<style scoped>
.osc-banner-wrapper {
  width: 100%;
  margin-bottom: 12px;
  padding: 2px;
  border-radius: 12px;
  animation: wrapperGlow 0.6s ease-in-out infinite alternate;
}

@keyframes wrapperGlow {
  from { box-shadow: 0 0 8px rgba(255, 180, 0, 0.4); }
  to   { box-shadow: 0 0 24px rgba(255, 100, 0, 0.8); }
}

.osc-warning-banner {
  position: relative;
  display: flex;
  gap: 16px;
  padding: 16px 20px;
  border-radius: 10px;
  overflow: hidden;
  background: linear-gradient(135deg, #ffcc00 0%, #ffaa00 50%, #ff8800 100%);
  border: 2px solid #ff6600;
  color: #1a1a1a;
}

.banner-warning {
  background: linear-gradient(135deg, #ffe066 0%, #ffd633 50%, #ffcc00 100%);
  border-color: #e6b800;
}

.banner-alert {
  background: linear-gradient(135deg, #ffb84d 0%, #ff9933 50%, #ff7733 100%);
  border-color: #cc5500;
  animation: bannerFlash 0.4s ease-in-out infinite alternate;
}

.banner-emergency {
  background: linear-gradient(135deg, #ff6666 0%, #ff3333 40%, #cc0000 100%);
  border-color: #990000;
  color: #fff !important;
  animation: emergencyFlash 0.18s steps(2) infinite;
}

.pulse-high .pulse-overlay {
  animation: pulseHigh 0.25s ease-in-out infinite;
}

@keyframes bannerFlash {
  from { filter: brightness(1); }
  to   { filter: brightness(1.35); }
}

@keyframes emergencyFlash {
  0%, 100% {
    background: linear-gradient(135deg, #ff6666 0%, #ff3333 40%, #cc0000 100%);
  }
  50% {
    background: linear-gradient(135deg, #ffff66 0%, #ffcc00 40%, #ff9900 100%);
    color: #1a1a1a !important;
  }
}

.pulse-overlay {
  position: absolute;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(255, 255, 255, 0.2);
  pointer-events: none;
  opacity: 0;
}

.pulse-overlay.delay-1 { animation-delay: 0.08s; }
.pulse-overlay.delay-2 { animation-delay: 0.16s; }

@keyframes pulseHigh {
  0%   { opacity: 0; }
  25%  { opacity: 0.6; }
  50%  { opacity: 0; }
  75%  { opacity: 0.4; }
  100% { opacity: 0; }
}

.banner-icon {
  flex-shrink: 0;
  width: 48px;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.15);
  border-radius: 50%;
  animation: iconShake 0.25s ease-in-out infinite;
  color: inherit;
}

.banner-emergency .banner-icon {
  background: rgba(255, 255, 255, 0.25);
}

@keyframes iconShake {
  0%, 100% { transform: rotate(-4deg) scale(1); }
  25%      { transform: rotate(4deg) scale(1.05); }
  50%      { transform: rotate(-4deg) scale(1); }
  75%      { transform: rotate(4deg) scale(1.05); }
}

.banner-content {
  flex: 1;
  min-width: 0;
  z-index: 1;
}

.banner-title {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
  font-size: 16px;
  font-weight: 600;
}

.severity-badge {
  padding: 4px 12px;
  border-radius: 20px;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.badge-WARNING   { background: #e6b800; color: #1a1a1a; }
.badge-ALERT     { background: #cc5500; color: #fff; }
.badge-EMERGENCY { background: #990000; color: #ffff00; animation: textBlink 0.2s steps(2) infinite; }

@keyframes textBlink {
  0%, 100% { color: #ffff00; }
  50%      { color: #ff0000; }
}

.osc-type {
  color: #cc0000;
  font-size: 17px;
}
.banner-emergency .osc-type { color: #ffff00; }

.section-count { flex: 1; }

.alert-sections {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 8px;
}

.section-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 10px;
  background: rgba(255, 255, 255, 0.35);
  border-radius: 6px;
  font-size: 13px;
  font-family: 'Consolas', 'Monaco', monospace;
}

.banner-emergency .section-item {
  background: rgba(0, 0, 0, 0.25);
}

.section-item.sev-EMERGENCY {
  background: rgba(255, 0, 0, 0.4);
  border: 1px solid rgba(255, 255, 0, 0.5);
}
.banner-emergency .section-item.sev-EMERGENCY {
  background: rgba(255, 255, 0, 0.3);
  color: #1a1a1a;
}

.sec-name { font-weight: 700; min-width: 110px; }
.sec-mode { opacity: 0.9; }
.sec-damping.neg-damp { color: #cc0000; font-weight: 700; }
.banner-emergency .sec-damping.neg-damp { color: #ff0000; font-weight: 700; }
.section-item.sev-EMERGENCY .sec-damping.neg-damp { animation: textBlink 0.2s steps(2) infinite; }

.sec-gradient.neg-grad { color: #990000; }
.sec-conf { opacity: 0.85; }
.sec-diverge {
  margin-left: auto;
  padding: 2px 8px;
  background: rgba(255, 0, 0, 0.5);
  color: #fff;
  border-radius: 4px;
  font-weight: 700;
  animation: divergeBlink 0.3s steps(2) infinite;
}

@keyframes divergeBlink {
  0%, 100% { background: rgba(255, 0, 0, 0.7); }
  50%      { background: rgba(255, 200, 0, 0.9); color: #1a1a1a; }
}

.banner-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 4px;
  border-top: 1px dashed rgba(0, 0, 0, 0.25);
  font-size: 13px;
}

.banner-emergency .banner-footer {
  border-top-color: rgba(255, 255, 255, 0.4);
}

.recommend strong {
  color: #990000;
  font-weight: 700;
}
.banner-emergency .recommend strong { color: #ffff00; }

.control-status {
  padding: 4px 12px;
  background: rgba(0, 150, 0, 0.6);
  color: #fff;
  border-radius: 6px;
  font-weight: 600;
  animation: ctrlPulse 0.8s ease-in-out infinite alternate;
}

@keyframes ctrlPulse {
  from { box-shadow: 0 0 4px rgba(0, 200, 0, 0.5); }
  to   { box-shadow: 0 0 16px rgba(0, 255, 0, 0.9); }
}
</style>
