<template>
  <Teleport to="body">
    <div v-if="isVisible" class="modal-backdrop">
      <div class="modal-container" :class="'sev-' + severity">
        <div class="modal-header">
          <div class="header-icon">
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
              <line x1="12" y1="9" x2="12" y2="13"/>
              <line x1="12" y1="17" x2="12.01" y2="17"/>
            </svg>
          </div>
          <div class="header-text">
            <div class="header-severity">{{ severityLabel }}</div>
            <div class="header-title">
              普罗尼模态分析确诊报告
              <span class="blink-cursor">_</span>
            </div>
            <div class="header-time">
              检测时间：{{ formatTime(alert?.timestamp) }}
            </div>
          </div>
          <div class="header-countdown" v-if="countdown > 0">
            <div class="countdown-num">{{ countdown }}</div>
            <div class="countdown-label">秒后自动确认</div>
          </div>
        </div>

        <div class="modal-body">
          <div class="section-block">
            <div class="section-title">📡 输电断面定位</div>
            <div class="section-grid">
              <div class="grid-item">
                <span class="item-label">断面名称</span>
                <span class="item-value highlight">{{ alert?.sectionName }}</span>
              </div>
              <div class="grid-item">
                <span class="item-label">送端电站</span>
                <span class="item-value">{{ alert?.fromStation }}</span>
              </div>
              <div class="grid-item">
                <span class="item-label">受端电站</span>
                <span class="item-value">{{ alert?.toStation }}</span>
              </div>
              <div class="grid-item">
                <span class="item-label">当前输送功率</span>
                <span class="item-value">{{ formatMW(alert?.activePowerMW) }}</span>
              </div>
            </div>
          </div>

          <div class="section-block danger-block">
            <div class="section-title danger-title">
              ⚠️ 主导振荡模态辨识结果（Prony Analysis）
            </div>

            <div class="dominant-mode-card">
              <div class="mode-row">
                <div class="mode-label">振荡频率 f₀</div>
                <div class="mode-value big-value">
                  {{ alert?.dominantMode?.frequency?.toFixed(4) }}
                  <span class="unit">Hz</span>
                </div>
                <div class="mode-bar">
                  <div class="bar-fill freq-bar" :style="{width: freqPercent + '%'}"></div>
                </div>
              </div>

              <div class="mode-row">
                <div class="mode-label">物理阻尼比 ζ</div>
                <div class="mode-value big-value danger-value" :class="{'blink-value': isNegativeDamping}">
                  {{ (alert?.dominantMode?.dampingRatio * 100)?.toFixed(3) }}
                  <span class="unit">%</span>
                </div>
                <div class="mode-bar">
                  <div class="bar-fill damp-bar" :class="{'neg-bar': isNegativeDamping}" :style="{width: dampPercent + '%'}"></div>
                </div>
              </div>

              <div class="mode-row">
                <div class="mode-label">阻尼因子 σ</div>
                <div class="mode-value">
                  {{ alert?.dominantMode?.dampingFactor?.toFixed(4) }}
                  <span class="unit">s⁻¹</span>
                </div>
              </div>

              <div class="mode-row">
                <div class="mode-label">振荡幅值</div>
                <div class="mode-value">
                  {{ formatMW(alert?.powerOscAmplitude) }}
                </div>
              </div>

              <div class="mode-row">
                <div class="mode-label">模态能量占比</div>
                <div class="mode-value">
                  {{ (alert?.dominantMode?.energyRatio * 100)?.toFixed(1) }}
                  <span class="unit">%</span>
                </div>
              </div>
            </div>

            <div class="gradient-box">
              <div class="gradient-label">
                📈 阻尼衰减微分梯度（dζ/dt）：
                <span class="gradient-value" :class="{'neg-gradient': isGradientNeg}">
                  {{ (alert?.dampingGradient * 1e5)?.toFixed(4) }} × 10⁻⁵
                </span>
              </div>
              <div class="gradient-bar">
                <div class="gradient-track">
                  <div class="gradient-zero"></div>
                  <div class="gradient-fill" :class="{'fill-neg': isGradientNeg}" :style="gradientStyle"></div>
                </div>
              </div>
              <div class="gradient-desc" v-if="isGradientNeg">
                梯度持续为负 → 阻尼加速衰减 → 振荡发散不可避免 → 需要立即控制！
              </div>
            </div>
          </div>

          <div class="section-block" v-if="alert?.detectedModes && alert.detectedModes.length > 1">
            <div class="section-title">📊 全部检测模态（{{ alert.detectedModes.length }} 个）</div>
            <table class="modes-table">
              <thead>
                <tr>
                  <th>频率 (Hz)</th>
                  <th>阻尼比 ζ (%)</th>
                  <th>阻尼因子 (s⁻¹)</th>
                  <th>能量占比</th>
                  <th>判定</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(m, i) in alert.detectedModes" :key="i" :class="{'dominant-row': i === 0, 'bad-row': m.dampingRatio < 0}">
                  <td class="mono">{{ m.frequency.toFixed(3) }}</td>
                  <td class="mono" :class="{'text-danger': m.dampingRatio < 0}">
                    {{ (m.dampingRatio * 100).toFixed(3) }}
                    <span v-if="m.dampingRatio < 0"> ↓</span>
                  </td>
                  <td class="mono">{{ m.dampingFactor.toFixed(4) }}</td>
                  <td>
                    <div class="mini-bar-wrap">
                      <div class="mini-bar" :style="{width: Math.min(m.energyRatio * 100, 100) + '%'}"></div>
                    </div>
                    {{ (m.energyRatio * 100).toFixed(1) }}%
                  </td>
                  <td>
                    <span v-if="m.dampingRatio < 0" class="badge-danger">负阻尼</span>
                    <span v-else-if="m.dampingRatio < 0.03" class="badge-warn">弱阻尼</span>
                    <span v-else class="badge-ok">正常</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <div class="section-block critical-block">
            <div class="section-title critical-title">🚨 系统稳定性判定</div>
            <div class="verdict-grid">
              <div class="verdict-item">
                <span class="v-label">负阻尼检测</span>
                <span class="v-value" :class="alert?.negativeDamping ? 'v-bad' : 'v-ok'">
                  {{ alert?.negativeDamping ? '⚠ 已检测到' : '正常' }}
                </span>
              </div>
              <div class="verdict-item">
                <span class="v-label">持续发散计数</span>
                <span class="v-value" :class="(alert?.divergenceCount || 0) >= 3 ? 'v-bad' : 'v-ok'">
                  {{ alert?.divergenceCount || 0 }} 次
                </span>
              </div>
              <div class="verdict-item">
                <span class="v-label">发散状态</span>
                <span class="v-value" :class="alert?.diverging ? 'v-bad blink' : 'v-ok'">
                  {{ alert?.diverging ? '💥 正在发散' : '稳定' }}
                </span>
              </div>
              <div class="verdict-item">
                <span class="v-label">置信度</span>
                <span class="v-value">{{ (alert?.confidenceLevel * 100)?.toFixed(1) }}%</span>
              </div>
              <div class="verdict-item full-width">
                <span class="v-label">相对功角摆开</span>
                <span class="v-value" :class="(alert?.angleSeparationDeg || 0) > 30 ? 'v-bad' : 'v-ok'">
                  {{ alert?.angleSeparationDeg?.toFixed(2) }}°
                </span>
              </div>
            </div>
          </div>

          <div class="section-block action-block">
            <div class="section-title action-title">
              ⚡ 推荐紧急控制措施（最高优先级）
            </div>
            <div class="action-text">{{ alert?.recommendedAction }}</div>

            <div v-if="controlAction" class="control-executed">
              <div class="ctrl-header">✅ 已自动执行控制指令</div>
              <div class="ctrl-grid">
                <div class="ctrl-item">
                  <span class="ctrl-label">指令编号</span>
                  <span class="ctrl-value mono">{{ controlAction.actionId }}</span>
                </div>
                <div class="ctrl-item">
                  <span class="ctrl-label">优先级</span>
                  <span class="ctrl-value badge-danger">P{{ controlAction.priority }}（最高）</span>
                </div>
                <div class="ctrl-item">
                  <span class="ctrl-label">切机容量</span>
                  <span class="ctrl-value">{{ formatMW(controlAction.tripAmountMW) }}</span>
                </div>
                <div class="ctrl-item">
                  <span class="ctrl-label">制动容量</span>
                  <span class="ctrl-value">{{ formatMW(controlAction.brakeAmountMW) }}</span>
                </div>
                <div class="ctrl-item full-width">
                  <span class="ctrl-label">切除机组</span>
                  <span class="ctrl-value">{{ controlAction.tripGenerators?.join(', ') || '-' }}</span>
                </div>
                <div class="ctrl-item full-width">
                  <span class="ctrl-label">投入制动电阻</span>
                  <span class="ctrl-value">{{ controlAction.brakingResistors?.join(', ') || '-' }}</span>
                </div>
                <div class="ctrl-item">
                  <span class="ctrl-label">执行耗时</span>
                  <span class="ctrl-value">{{ controlAction.executionTimeMs }} ms</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="modal-footer">
          <div class="warning-text">
            ⚠ 此为电网稳定性紧急确诊报告，请调度员立即采取措施。本弹窗不可手动取消。
          </div>
          <button
            class="confirm-btn"
            :disabled="countdown > 0 || !canDismiss"
            @click="dismiss"
          >
            <span v-if="countdown > 0">{{ countdown }} 秒后可确认...</span>
            <span v-else>✅ 我已知晓并采取措施（确认）</span>
          </button>
        </div>

        <div class="corner-decoration tl"></div>
        <div class="corner-decoration tr"></div>
        <div class="corner-decoration bl"></div>
        <div class="corner-decoration br"></div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>import { ref, computed, watch, onMounted, onUnmounted } from 'vue';
const props = defineProps({
 alert: {
 type: Object,
 default: null
 },
 controlAction: {
 type: Object,
 default: null
 }
});
const emit = defineEmits(['dismiss']);
const isVisible = ref(false);
const countdown = ref(10);
let countdownTimer = null;
const severity = computed(() => props.alert?.severity || 'ALERT');
const severityLabel = computed(() => {
 const map = {
 'WARNING': '🟡 低阻尼预警',
 'ALERT': '🟠 负阻尼告警',
 'EMERGENCY': '🔴 紧急：振荡发散'
 };
 return map[severity.value] || '告警';
});
const isNegativeDamping = computed(() =>
 (props.alert?.dominantMode?.dampingRatio || 0) < 0
);
const isGradientNeg = computed(() => (props.alert?.dampingGradient || 0) < 0);
const canDismiss = computed(() => {
 return (severity.value === 'WARNING' ||
 (severity.value === 'ALERT' && !props.alert?.diverging));
});
const freqPercent = computed(() => {
 const f = props.alert?.dominantMode?.frequency || 0;
 return Math.min((f / 2.5) * 100, 100);
});
const dampPercent = computed(() => {
 const d = props.alert?.dominantMode?.dampingRatio || 0;
 if (d >= 0)
 return Math.min(d * 100 * 20, 100);
 return Math.min(Math.abs(d) * 100 * 10, 50);
});
const gradientStyle = computed(() => {
 const g = props.alert?.dampingGradient || 0;
 const maxG = 1e-4;
 const norm = Math.max(-1, Math.min(1, g / maxG));
 const percent = Math.abs(norm) * 50;
 if (norm < 0) {
 return { width: percent + '%', right: '50%' };
 }
 return { width: percent + '%', left: '50%' };
});
function formatTime(t) {
 if (!t)
 return '-';
 const d = new Date(t);
 return d.toLocaleString('zh-CN', { hour12: false });
}
function formatMW(v) {
 if (v === undefined || v === null || isNaN(v))
 return '-';
 if (Math.abs(v) >= 1000)
 return (v / 1000).toFixed(2) + ' GW';
 return v.toFixed(1) + ' MW';
}
function startCountdown() {
 countdown.value = 10;
 if (countdownTimer)
 clearInterval(countdownTimer);
 countdownTimer = setInterval(() => {
 if (countdown.value > 0) {
 countdown.value--;
 }
 }, 1000);
}
function dismiss() {
 if (countdown.value > 0)
 return;
 if (!canDismiss.value && !props.alert?.diverging)
 return;
 if (severity.value === 'EMERGENCY' && props.alert?.diverging && countdown.value > 0)
 return;
 isVisible.value = false;
 if (countdownTimer) {
 clearInterval(countdownTimer);
 countdownTimer = null;
 }
 emit('dismiss');
}
watch(() => props.alert, (newAlert) => {
 if (newAlert && (newAlert.severity === 'ALERT' || newAlert.severity === 'EMERGENCY')) {
 isVisible.value = true;
 startCountdown();
 }
}, { immediate: true, deep: true });
onMounted(() => {
 if (props.alert) {
 isVisible.value = true;
 startCountdown();
 }
});
onUnmounted(() => {
 if (countdownTimer)
 clearInterval(countdownTimer);
});
</script>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.75);
  backdrop-filter: blur(4px);
  z-index: 10000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  animation: fadeIn 0.2s ease-out;
}

@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

.modal-container {
  position: relative;
  width: 100%;
  max-width: 860px;
  max-height: 90vh;
  background: linear-gradient(180deg, #1a1a2e 0%, #16213e 100%);
  border-radius: 16px;
  overflow-y: auto;
  border: 3px solid;
  box-shadow: 0 0 60px rgba(255, 100, 0, 0.4);
  animation: modalPop 0.35s cubic-bezier(0.34, 1.56, 0.64, 1);
}

@keyframes modalPop {
  0%   { transform: scale(0.7) translateY(30px); opacity: 0; }
  100% { transform: scale(1) translateY(0); opacity: 1; }
}

.modal-container.sev-WARNING   { border-color: #ffcc00; box-shadow: 0 0 60px rgba(255, 204, 0, 0.4); }
.modal-container.sev-ALERT     { border-color: #ff7700; box-shadow: 0 0 70px rgba(255, 119, 0, 0.5); animation: modalPop 0.35s cubic-bezier(0.34, 1.56, 0.64, 1), shake 0.5s; }
.modal-container.sev-EMERGENCY { border-color: #ff0000; box-shadow: 0 0 90px rgba(255, 0, 0, 0.6); animation: modalPop 0.35s cubic-bezier(0.34, 1.56, 0.64, 1), shake 0.3s, borderFlash 0.4s steps(2) infinite; }

@keyframes shake {
  0%, 100% { transform: translateX(0); }
  10%, 30%, 50%, 70%, 90% { transform: translateX(-8px); }
  20%, 40%, 60%, 80% { transform: translateX(8px); }
}

@keyframes borderFlash {
  0%, 100% { border-color: #ff0000; box-shadow: 0 0 90px rgba(255, 0, 0, 0.6); }
  50%      { border-color: #ffff00; box-shadow: 0 0 90px rgba(255, 255, 0, 0.8); }
}

.corner-decoration {
  position: absolute;
  width: 40px;
  height: 40px;
  border: 3px solid;
}
.sev-WARNING   .corner-decoration { border-color: #ffcc00; }
.sev-ALERT     .corner-decoration { border-color: #ff7700; }
.sev-EMERGENCY .corner-decoration { border-color: #ff0000; animation: cornerBlink 0.3s steps(2) infinite; }
.corner-decoration.tl { top: 8px; left: 8px; border-right: none; border-bottom: none; }
.corner-decoration.tr { top: 8px; right: 8px; border-left: none; border-bottom: none; }
.corner-decoration.bl { bottom: 8px; left: 8px; border-right: none; border-top: none; }
.corner-decoration.br { bottom: 8px; right: 8px; border-left: none; border-top: none; }

@keyframes cornerBlink {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.modal-header {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 20px 24px;
  background: linear-gradient(135deg, rgba(255, 119, 0, 0.3) 0%, rgba(255, 0, 0, 0.15) 100%);
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
}
.sev-WARNING .modal-header { background: linear-gradient(135deg, rgba(255, 204, 0, 0.3) 0%, rgba(255, 170, 0, 0.15) 100%); }
.sev-EMERGENCY .modal-header { background: linear-gradient(135deg, rgba(255, 0, 0, 0.4) 0%, rgba(180, 0, 0, 0.25) 100%); animation: headerFlash 0.4s steps(2) infinite; }

@keyframes headerFlash {
  0%, 100% { background: linear-gradient(135deg, rgba(255, 0, 0, 0.4), rgba(180, 0, 0, 0.25)); }
  50%      { background: linear-gradient(135deg, rgba(255, 255, 0, 0.4), rgba(255, 170, 0, 0.25)); }
}

.header-icon {
  flex-shrink: 0;
  color: #ff7700;
  animation: iconSpin 2s linear infinite;
}
.sev-WARNING   .header-icon { color: #ffcc00; }
.sev-EMERGENCY .header-icon { color: #ff0000; animation: iconSpin 0.8s linear infinite, iconBlink 0.3s steps(2) infinite; }

@keyframes iconSpin {
  0%, 100% { transform: scale(1); }
  50%      { transform: scale(1.15); }
}

@keyframes iconBlink {
  0%, 100% { opacity: 1; }
  50%      { opacity: 0.5; }
}

.header-text { flex: 1; min-width: 0; }
.header-severity {
  font-size: 13px;
  font-weight: 700;
  color: #ffaa00;
  margin-bottom: 4px;
  letter-spacing: 1px;
}
.sev-WARNING   .header-severity { color: #ffd633; }
.sev-EMERGENCY .header-severity { color: #ff3333; }

.header-title {
  font-size: 22px;
  font-weight: 800;
  color: #fff;
  margin-bottom: 4px;
}
.blink-cursor {
  color: #00ff00;
  animation: blink 0.5s steps(2) infinite;
  font-weight: 400;
}
@keyframes blink {
  0%, 100% { opacity: 1; }
  50%      { opacity: 0; }
}

.header-time {
  font-size: 13px;
  color: #aaa;
}

.header-countdown {
  flex-shrink: 0;
  text-align: center;
  padding: 8px 16px;
  background: rgba(255, 0, 0, 0.3);
  border-radius: 12px;
  border: 2px solid #ff3333;
}
.countdown-num {
  font-size: 32px;
  font-weight: 900;
  color: #ff3333;
  animation: countdownPulse 1s ease-in-out infinite;
  line-height: 1;
}
@keyframes countdownPulse {
  0%, 100% { transform: scale(1); }
  50%      { transform: scale(1.2); color: #ff0000; }
}
.countdown-label {
  font-size: 11px;
  color: #ffaaaa;
  margin-top: 2px;
}

.modal-body {
  padding: 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-block {
  background: rgba(255, 255, 255, 0.04);
  border-radius: 12px;
  padding: 16px;
  border: 1px solid rgba(255, 255, 255, 0.08);
}
.danger-block {
  background: linear-gradient(180deg, rgba(255, 80, 0, 0.15) 0%, rgba(255, 255, 255, 0.04) 100%);
  border-color: rgba(255, 119, 0, 0.4);
}
.sev-EMERGENCY .danger-block {
  border-color: rgba(255, 0, 0, 0.6);
  animation: dangerFlash 0.8s ease-in-out infinite alternate;
}
@keyframes dangerFlash {
  from { box-shadow: inset 0 0 10px rgba(255, 0, 0, 0.2); }
  to   { box-shadow: inset 0 0 30px rgba(255, 50, 0, 0.4); }
}
.critical-block {
  background: linear-gradient(180deg, rgba(200, 0, 0, 0.2) 0%, rgba(255, 255, 255, 0.04) 100%);
  border-color: rgba(255, 50, 50, 0.5);
}
.action-block {
  background: linear-gradient(180deg, rgba(0, 150, 80, 0.18) 0%, rgba(255, 255, 255, 0.04) 100%);
  border-color: rgba(0, 200, 100, 0.5);
}

.section-title {
  font-size: 15px;
  font-weight: 700;
  color: #fff;
  margin-bottom: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
}
.danger-title   { color: #ff7733; }
.critical-title { color: #ff4444; }
.action-title   { color: #33ff77; }

.section-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 10px;
}
.grid-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 14px;
  background: rgba(0, 0, 0, 0.3);
  border-radius: 8px;
}
.item-label { font-size: 12px; color: #888; }
.item-value { font-size: 15px; color: #ddd; font-weight: 600; }
.item-value.highlight { color: #ffaa33; font-size: 17px; }

.dominant-mode-card {
  background: rgba(0, 0, 0, 0.35);
  border-radius: 10px;
  padding: 14px;
  margin-bottom: 12px;
  border: 1px solid rgba(255, 119, 0, 0.3);
}
.sev-EMERGENCY .dominant-mode-card {
  border-color: rgba(255, 0, 0, 0.5);
  animation: cardFlash 0.5s ease-in-out infinite alternate;
}
@keyframes cardFlash {
  from { border-color: rgba(255, 0, 0, 0.3); }
  to   { border-color: rgba(255, 100, 0, 0.8); }
}

.mode-row {
  display: grid;
  grid-template-columns: 140px 160px 1fr;
  align-items: center;
  gap: 12px;
  padding: 6px 0;
  border-bottom: 1px dashed rgba(255, 255, 255, 0.08);
}
.mode-row:last-child { border-bottom: none; }
.mode-label { font-size: 13px; color: #aaa; }
.mode-value {
  font-size: 15px;
  color: #eee;
  font-family: 'Consolas', monospace;
}
.mode-value.big-value { font-size: 20px; font-weight: 700; }
.mode-value.danger-value { color: #ff5555; }
.mode-value.blink-value { animation: valueBlink 0.35s steps(2) infinite; }
@keyframes valueBlink {
  0%, 100% { color: #ff3333; }
  50%      { color: #ffff00; }
}
.unit { font-size: 12px; color: #888; margin-left: 4px; }

.mode-bar {
  height: 8px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  overflow: hidden;
}
.bar-fill { height: 100%; border-radius: 4px; transition: width 0.3s; }
.freq-bar { background: linear-gradient(90deg, #00ccff, #0088ff); }
.damp-bar { background: linear-gradient(90deg, #33ff77, #00cc44); }
.damp-bar.neg-bar { background: linear-gradient(90deg, #ff5555, #ff0000); animation: dampNegPulse 0.4s ease-in-out infinite alternate; }
@keyframes dampNegPulse {
  from { filter: brightness(1); }
  to   { filter: brightness(1.5); }
}

.gradient-box {
  background: rgba(0, 0, 0, 0.35);
  border-radius: 10px;
  padding: 14px;
}
.gradient-label {
  font-size: 14px;
  color: #ccc;
  margin-bottom: 10px;
}
.gradient-value { color: #33ff77; font-weight: 700; font-family: 'Consolas', monospace; }
.gradient-value.neg-gradient { color: #ff5555; animation: gradBlink 0.3s steps(2) infinite; }
@keyframes gradBlink {
  0%, 100% { color: #ff3333; }
  50%      { color: #ffff00; }
}

.gradient-bar { padding: 0 10px; }
.gradient-track {
  position: relative;
  height: 18px;
  background: rgba(255, 255, 255, 0.08);
  border-radius: 9px;
  overflow: hidden;
}
.gradient-zero {
  position: absolute;
  left: 50%;
  top: 0; bottom: 0;
  width: 2px;
  background: rgba(255, 255, 255, 0.4);
}
.gradient-fill {
  position: absolute;
  top: 0; bottom: 0;
  background: linear-gradient(90deg, #00ff88, #00cc44);
  border-radius: 9px;
}
.gradient-fill.fill-neg {
  background: linear-gradient(90deg, #ff4444, #ff0000);
  animation: fillNegFlash 0.3s steps(2) infinite;
}
@keyframes fillNegFlash {
  0%, 100% { opacity: 1; }
  50%      { opacity: 0.6; }
}

.gradient-desc {
  margin-top: 10px;
  font-size: 13px;
  color: #ff7777;
  font-weight: 600;
  padding: 8px;
  background: rgba(255, 0, 0, 0.15);
  border-radius: 6px;
  border-left: 3px solid #ff3333;
}

.modes-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.modes-table th, .modes-table td {
  padding: 8px 10px;
  text-align: left;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}
.modes-table th {
  background: rgba(0, 0, 0, 0.35);
  color: #aaa;
  font-weight: 600;
  font-size: 12px;
}
.modes-table tr.dominant-row {
  background: rgba(255, 170, 0, 0.12);
}
.modes-table tr.bad-row {
  background: rgba(255, 50, 50, 0.12);
}
.mono { font-family: 'Consolas', monospace; }
.text-danger { color: #ff5555; font-weight: 600; }

.mini-bar-wrap {
  display: inline-block;
  width: 70px;
  height: 6px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 3px;
  overflow: hidden;
  margin-right: 8px;
  vertical-align: middle;
}
.mini-bar { height: 100%; background: linear-gradient(90deg, #00aaff, #0066ff); }

.badge-danger { background: rgba(255, 40, 40, 0.8); color: #fff; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.badge-warn   { background: rgba(255, 170, 0, 0.8); color: #1a1a1a; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }
.badge-ok     { background: rgba(0, 180, 80, 0.8); color: #fff; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 600; }

.verdict-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 10px;
}
.verdict-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px;
  background: rgba(0, 0, 0, 0.3);
  border-radius: 8px;
}
.verdict-item.full-width { grid-column: span 2; }
.v-label { color: #aaa; font-size: 13px; }
.v-value { font-weight: 700; font-size: 15px; color: #eee; }
.v-value.v-ok  { color: #33ff77; }
.v-value.v-bad { color: #ff5555; animation: vBadBlink 0.4s steps(2) infinite; }
.v-value.v-bad.blink { animation: vBadBlink 0.25s steps(2) infinite; }
@keyframes vBadBlink {
  0%, 100% { color: #ff3333; }
  50%      { color: #ffff00; }
}

.action-text {
  padding: 14px;
  background: rgba(0, 0, 0, 0.35);
  border-radius: 8px;
  font-size: 15px;
  color: #aaffaa;
  font-weight: 600;
  line-height: 1.5;
  border-left: 4px solid #00cc66;
}

.control-executed {
  margin-top: 12px;
  padding: 14px;
  background: rgba(0, 180, 80, 0.15);
  border-radius: 10px;
  border: 1px solid rgba(0, 220, 100, 0.5);
  animation: ctrlGlow 1s ease-in-out infinite alternate;
}
@keyframes ctrlGlow {
  from { box-shadow: inset 0 0 10px rgba(0, 255, 100, 0.1); }
  to   { box-shadow: inset 0 0 25px rgba(0, 255, 100, 0.3); }
}
.ctrl-header {
  font-size: 14px;
  font-weight: 700;
  color: #33ff77;
  margin-bottom: 10px;
}
.ctrl-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
}
.ctrl-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: rgba(0, 0, 0, 0.3);
  border-radius: 6px;
}
.ctrl-item.full-width { grid-column: span 2; }
.ctrl-label { color: #88ccaa; font-size: 12px; }
.ctrl-value { color: #ddffdd; font-weight: 600; font-size: 13px; }

.modal-footer {
  padding: 16px 24px;
  border-top: 1px solid rgba(255, 255, 255, 0.1);
  background: rgba(0, 0, 0, 0.4);
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.warning-text {
  font-size: 12px;
  color: #ff9999;
  text-align: center;
  padding: 8px;
  background: rgba(255, 50, 50, 0.1);
  border-radius: 6px;
}

.confirm-btn {
  padding: 14px 24px;
  font-size: 16px;
  font-weight: 700;
  background: linear-gradient(135deg, #00aa44, #00cc66);
  color: #fff;
  border: none;
  border-radius: 10px;
  cursor: pointer;
  transition: all 0.2s;
}
.confirm-btn:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 200, 100, 0.4);
}
.confirm-btn:disabled {
  background: rgba(100, 100, 100, 0.4);
  color: #888;
  cursor: not-allowed;
}

::-webkit-scrollbar { width: 10px; }
::-webkit-scrollbar-track { background: rgba(0, 0, 0, 0.2); }
::-webkit-scrollbar-thumb { background: rgba(255, 119, 0, 0.5); border-radius: 5px; }
::-webkit-scrollbar-thumb:hover { background: rgba(255, 119, 0, 0.7); }
</style>
