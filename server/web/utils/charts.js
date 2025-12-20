// PrintMaster Server - Chart Utilities
// Extracted from app.js for modularity

/**
 * Default color palette for fleet series charts
 */
const FLEET_SERIES_COLORS = ['#7dd3fc', '#f472b6', '#facc15', '#34d399'];

/**
 * Format a number for display (with locale formatting)
 * @param {number|string} value - The value to format
 * @returns {string} Formatted string
 */
function formatChartNumber(value) {
    if (typeof value === 'number' && isFinite(value)) {
        return value.toLocaleString();
    }
    if (typeof value === 'string' && value.trim() !== '') {
        return value;
    }
    return '—';
}

/**
 * Determine appropriate time format based on the time range
 * @param {number} timeRangeMs - Time range in milliseconds
 * @returns {function} Formatter function that takes a timestamp
 */
function createTimeFormatter(timeRangeMs) {
    return (timestamp) => {
        const d = new Date(timestamp);
        if (timeRangeMs <= 24 * 60 * 60 * 1000) {
            // Within 24 hours: show HH:MM
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
        } else if (timeRangeMs <= 7 * 24 * 60 * 60 * 1000) {
            // Within a week: show day + time
            return d.toLocaleDateString([], { weekday: 'short' }) + ' ' + 
                   d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
        } else {
            // Longer: show month/day
            return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
        }
    };
}

/**
 * Draw X-axis time labels on a chart
 * @param {CanvasRenderingContext2D} ctx - Canvas context
 * @param {Object} params - Drawing parameters
 */
function drawTimeAxis(ctx, { minTime, maxTime, mapX, padding, width, height, formatTimeLabel }) {
    ctx.fillStyle = 'rgba(255,255,255,0.5)';
    ctx.font = '9px monospace';
    ctx.textAlign = 'center';
    const numTimeLabels = Math.min(6, Math.max(2, Math.floor(width / 80)));
    for (let i = 0; i <= numTimeLabels; i++) {
        const t = minTime + (maxTime - minTime) * (i / numTimeLabels);
        const x = mapX(t);
        const label = formatTimeLabel(t);
        ctx.fillText(label, x, padding.top + height + 14);
        
        // Draw tick mark
        ctx.strokeStyle = 'rgba(255,255,255,0.15)';
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(x, padding.top + height);
        ctx.lineTo(x, padding.top + height + 4);
        ctx.stroke();
    }
}

/**
 * Draw a single-axis fleet time series chart
 * @param {HTMLCanvasElement} canvas - The canvas element to draw on
 * @param {Array} seriesList - Array of series objects with { label, color, points }
 * @param {Object} options - Optional { formatY, label }
 */
function drawFleetChart(canvas, seriesList, options) {
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, rect.width, rect.height);

    const formatY = options?.formatY || ((v) => formatChartNumber(Math.round(v)));

    const points = seriesList.flatMap(s => s.points || []);
    if (points.length === 0) {
        ctx.fillStyle = 'rgba(255,255,255,0.6)';
        ctx.font = '12px sans-serif';
        ctx.fillText('No data', 12, rect.height / 2);
        return;
    }

    const minTime = Math.min(...points.map(p => p.time));
    const maxTime = Math.max(...points.map(p => p.time));
    const minValue = 0;
    const maxValue = Math.max(...points.map(p => p.value)) || 1;
    const padding = { top: 20, right: 16, bottom: 36, left: 60 };
    const width = rect.width - padding.left - padding.right;
    const height = rect.height - padding.top - padding.bottom;

    const timeRangeMs = maxTime - minTime;
    const formatTimeLabel = createTimeFormatter(timeRangeMs);

    const mapX = (time) => padding.left + ((time - minTime) / Math.max(1, maxTime - minTime)) * width;
    const mapY = (value) => padding.top + height - ((value - minValue) / Math.max(1, maxValue - minValue)) * height;

    // Draw horizontal grid lines
    ctx.strokeStyle = 'rgba(255,255,255,0.08)';
    ctx.lineWidth = 1;
    for (let i = 0; i <= 4; i++) {
        const y = padding.top + (height / 4) * i;
        ctx.beginPath();
        ctx.moveTo(padding.left, y);
        ctx.lineTo(padding.left + width, y);
        ctx.stroke();
    }

    // Draw chart border
    ctx.strokeStyle = 'rgba(255,255,255,0.12)';
    ctx.beginPath();
    ctx.moveTo(padding.left, padding.top);
    ctx.lineTo(padding.left, padding.top + height);
    ctx.lineTo(padding.left + width, padding.top + height);
    ctx.stroke();

    // Draw series lines
    seriesList.forEach((series, idx) => {
        const color = series.color || FLEET_SERIES_COLORS[idx % FLEET_SERIES_COLORS.length];
        const pts = (series.points || []).filter(p => Number.isFinite(p.time) && Number.isFinite(p.value));
        if (pts.length === 0) return;
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.beginPath();
        pts.forEach((pt, i) => {
            const x = mapX(pt.time);
            const y = mapY(pt.value);
            if (i === 0) {
                ctx.moveTo(x, y);
            } else {
                ctx.lineTo(x, y);
            }
        });
        ctx.stroke();
    });

    // Y-axis labels
    ctx.fillStyle = 'rgba(255,255,255,0.5)';
    ctx.font = '10px monospace';
    ctx.textAlign = 'right';
    for (let i = 0; i <= 4; i++) {
        const value = minValue + ((maxValue - minValue) / 4) * (4 - i);
        const y = padding.top + (height / 4) * i;
        ctx.fillText(formatY(value), padding.left - 6, y + 3);
    }

    // X-axis time labels
    drawTimeAxis(ctx, { minTime, maxTime, mapX, padding, width, height, formatTimeLabel });
}

/**
 * Draw a fleet chart with dual Y-axes: rates on left, cumulative totals on right.
 * @param {HTMLCanvasElement} canvas - The canvas element to draw on
 * @param {Array} rateSeriesList - Series for hourly rate data (left Y-axis, solid lines)
 * @param {Array} cumulativeSeriesList - Series for cumulative totals (right Y-axis, dashed lines)
 * @param {Object} options - Optional { formatY, label }
 */
function drawFleetChartDualAxis(canvas, rateSeriesList, cumulativeSeriesList, options) {
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, rect.width, rect.height);

    const formatY = options?.formatY || ((v) => formatChartNumber(Math.round(v)));

    const ratePoints = rateSeriesList.flatMap(s => s.points || []);
    const cumPoints = cumulativeSeriesList.flatMap(s => s.points || []);
    const allPoints = [...ratePoints, ...cumPoints];

    if (allPoints.length === 0) {
        ctx.fillStyle = 'rgba(255,255,255,0.6)';
        ctx.font = '12px sans-serif';
        ctx.fillText('No data', 12, rect.height / 2);
        return;
    }

    const minTime = Math.min(...allPoints.map(p => p.time));
    const maxTime = Math.max(...allPoints.map(p => p.time));

    // Rate axis (left) - for hourly deltas
    const rateMin = 0;
    const rateMax = Math.max(1, ...ratePoints.map(p => p.value));

    // Cumulative axis (right) - for running totals  
    const cumMin = cumPoints.length > 0 ? Math.min(...cumPoints.map(p => p.value)) : 0;
    const cumMax = cumPoints.length > 0 ? Math.max(...cumPoints.map(p => p.value)) : 1;
    // Add 5% padding to cumulative range for visual clarity
    const cumRange = cumMax - cumMin || 1;
    const cumMinAdj = Math.max(0, cumMin - cumRange * 0.05);
    const cumMaxAdj = cumMax + cumRange * 0.05;

    const padding = { top: 20, right: 65, bottom: 36, left: 60 };
    const width = rect.width - padding.left - padding.right;
    const height = rect.height - padding.top - padding.bottom;

    const timeRangeMs = maxTime - minTime;
    const formatTimeLabel = createTimeFormatter(timeRangeMs);

    const mapX = (time) => padding.left + ((time - minTime) / Math.max(1, maxTime - minTime)) * width;
    const mapYRate = (value) => padding.top + height - ((value - rateMin) / Math.max(1, rateMax - rateMin)) * height;
    const mapYCum = (value) => padding.top + height - ((value - cumMinAdj) / Math.max(1, cumMaxAdj - cumMinAdj)) * height;

    // Draw horizontal grid lines
    ctx.strokeStyle = 'rgba(255,255,255,0.08)';
    ctx.lineWidth = 1;
    for (let i = 0; i <= 4; i++) {
        const y = padding.top + (height / 4) * i;
        ctx.beginPath();
        ctx.moveTo(padding.left, y);
        ctx.lineTo(padding.left + width, y);
        ctx.stroke();
    }

    // Draw chart border
    ctx.strokeStyle = 'rgba(255,255,255,0.12)';
    ctx.beginPath();
    ctx.moveTo(padding.left, padding.top);
    ctx.lineTo(padding.left, padding.top + height);
    ctx.lineTo(padding.left + width, padding.top + height);
    ctx.lineTo(padding.left + width, padding.top);
    ctx.stroke();

    // Draw cumulative series first (dashed lines, behind rate lines)
    cumulativeSeriesList.forEach((series, idx) => {
        const color = series.color || '#9f7aea';
        const pts = (series.points || []).filter(p => Number.isFinite(p.time) && Number.isFinite(p.value));
        if (pts.length === 0) return;
        ctx.strokeStyle = color;
        ctx.lineWidth = 1.5;
        ctx.setLineDash([4, 3]);
        ctx.beginPath();
        pts.forEach((pt, i) => {
            const x = mapX(pt.time);
            const y = mapYCum(pt.value);
            if (i === 0) {
                ctx.moveTo(x, y);
            } else {
                ctx.lineTo(x, y);
            }
        });
        ctx.stroke();
        ctx.setLineDash([]);
    });

    // Draw rate series (solid lines, on top)
    rateSeriesList.forEach((series, idx) => {
        const color = series.color || FLEET_SERIES_COLORS[idx % FLEET_SERIES_COLORS.length];
        const pts = (series.points || []).filter(p => Number.isFinite(p.time) && Number.isFinite(p.value));
        if (pts.length === 0) return;
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.beginPath();
        pts.forEach((pt, i) => {
            const x = mapX(pt.time);
            const y = mapYRate(pt.value);
            if (i === 0) {
                ctx.moveTo(x, y);
            } else {
                ctx.lineTo(x, y);
            }
        });
        ctx.stroke();
    });

    // Left Y-axis labels (rate)
    ctx.fillStyle = 'rgba(255,255,255,0.6)';
    ctx.font = '10px monospace';
    ctx.textAlign = 'right';
    for (let i = 0; i <= 4; i++) {
        const value = rateMin + ((rateMax - rateMin) / 4) * (4 - i);
        const y = padding.top + (height / 4) * i;
        ctx.fillText(formatY(value), padding.left - 6, y + 3);
    }

    // Right Y-axis labels (cumulative)
    ctx.fillStyle = 'rgba(159, 122, 234, 0.7)';
    ctx.textAlign = 'left';
    for (let i = 0; i <= 4; i++) {
        const value = cumMinAdj + ((cumMaxAdj - cumMinAdj) / 4) * (4 - i);
        const y = padding.top + (height / 4) * i;
        ctx.fillText(formatY(value), padding.left + width + 6, y + 3);
    }

    // X-axis time labels
    drawTimeAxis(ctx, { minTime, maxTime, mapX, padding, width, height, formatTimeLabel });

    // Legend
    const legendY = padding.top - 6;
    ctx.font = '9px sans-serif';
    let legendX = padding.left;

    // Rate legend items
    rateSeriesList.forEach((series, idx) => {
        if (!series.label) return;
        const color = series.color || FLEET_SERIES_COLORS[idx % FLEET_SERIES_COLORS.length];
        ctx.fillStyle = color;
        ctx.fillRect(legendX, legendY - 6, 12, 2);
        ctx.fillStyle = 'rgba(255,255,255,0.6)';
        ctx.textAlign = 'left';
        ctx.fillText(series.label, legendX + 15, legendY);
        legendX += ctx.measureText(series.label).width + 25;
    });

    // Cumulative legend item (just one generic indicator)
    if (cumulativeSeriesList.length > 0 && cumulativeSeriesList.some(s => s.points?.length > 0)) {
        ctx.strokeStyle = 'rgba(159, 122, 234, 0.7)';
        ctx.lineWidth = 1.5;
        ctx.setLineDash([4, 3]);
        ctx.beginPath();
        ctx.moveTo(legendX, legendY - 5);
        ctx.lineTo(legendX + 12, legendY - 5);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.fillStyle = 'rgba(255,255,255,0.5)';
        ctx.fillText('Cumulative →', legendX + 15, legendY);
    }
}

// Export for use in app.js (these become globals when loaded via <script>)
// When we move to ES modules, these would be proper exports
if (typeof window !== 'undefined') {
    window.FLEET_SERIES_COLORS = FLEET_SERIES_COLORS;
    window.drawFleetChart = drawFleetChart;
    window.drawFleetChartDualAxis = drawFleetChartDualAxis;
}
