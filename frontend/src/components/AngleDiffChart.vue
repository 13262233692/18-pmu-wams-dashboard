<template>
  <div class="chart-container" ref="chartContainer">
    <div ref="chartRef" class="chart"></div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch, nextTick } from 'vue'
import * as echarts from 'echarts'

const props = defineProps({
  angleDiffHistory: {
    type: Array,
    default: () => []
  }
})

const chartContainer = ref(null)
const chartRef = ref(null)
let chartInstance = null
let resizeObserver = null

const sectionColors = {
  '华东-华北断面': '#4facfe',
  '华中-华东断面': '#00f2fe',
  '西北-华中断面': '#43e97b',
  '西南-华中断面': '#fa709a',
  '华南-华东断面': '#feca57',
  '东北-华北断面': '#a29bfe'
}

const initChart = () => {
  if (!chartRef.value) return

  chartInstance = echarts.init(chartRef.value, null, {
    renderer: 'canvas'
  })

  const option = {
    backgroundColor: 'transparent',
    animation: false,
    title: {
      text: '省际联络线功角差（度）',
      left: 'center',
      top: 0,
      textStyle: {
        color: '#7aa5e0',
        fontSize: 13,
        fontWeight: 'normal'
      }
    },
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(20, 30, 55, 0.95)',
      borderColor: 'rgba(100, 150, 255, 0.3)',
      borderWidth: 1,
      textStyle: {
        color: '#e0e6ff',
        fontSize: 12
      },
      formatter: (params) => {
        if (!params || params.length === 0) return ''
        let result = `<div style="margin-bottom:8px;color:#7aa5e0">${params[0].axisValue}</div>`
        params.forEach(p => {
          const value = p.value !== undefined && p.value !== null ? p.value[1]?.toFixed(3) : '--'
          result += `<div style="display:flex;align-items:center;gap:8px;margin:4px 0;">
            <span style="display:inline-block;width:10px;height:10px;border-radius:50%;background:${p.color}"></span>
            <span>${p.seriesName}</span>
            <span style="margin-left:auto;font-weight:600;color:#4facfe">${value}°</span>
          </div>`
        })
        return result
      }
    },
    legend: {
      data: [],
      top: 24,
      left: 'center',
      textStyle: {
        color: '#a0c0ff',
        fontSize: 11
      },
      itemWidth: 14,
      itemHeight: 2,
      itemGap: 16
    },
    grid: {
      left: 55,
      right: 20,
      top: 58,
      bottom: 32,
      containLabel: false
    },
    xAxis: {
      type: 'time',
      axisLine: {
        lineStyle: {
          color: 'rgba(100, 150, 255, 0.2)'
        }
      },
      axisTick: {
        show: false
      },
      axisLabel: {
        color: '#7aa5e0',
        fontSize: 10,
        formatter: (value) => {
          const date = new Date(value)
          return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}:${date.getSeconds().toString().padStart(2, '0')}`
        }
      },
      splitLine: {
        show: true,
        lineStyle: {
          color: 'rgba(100, 150, 255, 0.08)',
          type: 'dashed'
        }
      }
    },
    yAxis: {
      type: 'value',
      name: '相角差 (°)',
      nameTextStyle: {
        color: '#7aa5e0',
        fontSize: 11,
        padding: [0, 0, 0, -10]
      },
      axisLine: {
        show: false
      },
      axisTick: {
        show: false
      },
      axisLabel: {
        color: '#7aa5e0',
        fontSize: 10,
        formatter: (value) => value.toFixed(0)
      },
      splitLine: {
        show: true,
        lineStyle: {
          color: 'rgba(100, 150, 255, 0.08)',
          type: 'dashed'
        }
      },
      min: -60,
      max: 60
    },
    series: []
  }

  chartInstance.setOption(option)
}

const updateChart = () => {
  if (!chartInstance || props.angleDiffHistory.length === 0) return

  const sectionData = {}
  const sections = []

  props.angleDiffHistory.forEach(item => {
    if (!sectionData[item.sectionName]) {
      sectionData[item.sectionName] = []
      sections.push(item.sectionName)
    }
    const time = new Date(item.timestamp || item.unixNano / 1e6).getTime()
    sectionData[item.sectionName].push([time, item.angleDiff])
  })

  const series = sections.map(section => ({
    name: section,
    type: 'line',
    data: sectionData[section],
    smooth: true,
    smoothMonotone: 'x',
    showSymbol: false,
    lineStyle: {
      width: 2,
      color: sectionColors[section] || '#4facfe',
      shadowColor: sectionColors[section] || '#4facfe',
      shadowBlur: 8,
      shadowOffsetY: 2
    },
    sampling: 'lttb',
    large: true,
    largeThreshold: 500,
    progressive: 2000,
    progressiveThreshold: 3000
  }))

  chartInstance.setOption({
    legend: {
      data: sections
    },
    series: series
  }, {
    replaceMerge: ['series']
  })
}

const handleResize = () => {
  if (chartInstance) {
    chartInstance.resize()
  }
}

watch(() => props.angleDiffHistory.length, () => {
  nextTick(() => {
    updateChart()
  })
}, { immediate: false })

onMounted(() => {
  nextTick(() => {
    initChart()
    updateChart()

    if ('ResizeObserver' in window) {
      resizeObserver = new ResizeObserver(handleResize)
      if (chartContainer.value) {
        resizeObserver.observe(chartContainer.value)
      }
    }

    window.addEventListener('resize', handleResize)
  })
})

onUnmounted(() => {
  if (resizeObserver) {
    resizeObserver.disconnect()
  }
  window.removeEventListener('resize', handleResize)
  if (chartInstance) {
    chartInstance.dispose()
    chartInstance = null
  }
})
</script>

<style scoped>
.chart-container {
  width: 100%;
  height: 100%;
  min-height: 300px;
  flex: 1;
  display: flex;
  flex-direction: column;
}

.chart {
  width: 100%;
  height: 100%;
  min-height: 280px;
  flex: 1;
}
</style>
