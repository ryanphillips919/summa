const money = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });

function card(label, value) {
  return `<article class="card"><div class="label">${label}</div><div class="value">${value}</div></article>`;
}

function showError(message) {
  const error = document.getElementById("error");
  error.textContent = message;
  error.hidden = false;
}

function render(result) {
  const { summary, daily_cash_flow: actual, forecast, anomalies } = result;
  document.getElementById("summary").innerHTML = [
    card("Forecast model", summary.forecast_model),
    card("Baseline MAE", summary.baseline_validation_mae == null ? "Need more history" : money.format(summary.baseline_validation_mae)),
    card("Selected-model MAE", summary.selected_model_validation_mae == null ? "Need more history" : money.format(summary.selected_model_validation_mae)),
    card("Items to review", summary.anomaly_count),
  ].join("");

  const actualByDate = new Map(actual.map((point) => [point.date, point.net_cash_flow]));
  const forecastByDate = new Map(forecast.map((point) => [point.date, point.predicted_net_cash_flow]));
  const dates = [...actual.map((point) => point.date), ...forecast.map((point) => point.date)];
  const lower = forecast.map((point) => point.lower_95);
  const band = forecast.map((point) => point.upper_95 - point.lower_95);
  const anomalyDates = [...new Set(anomalies.map((item) => item.date))];

  const chart = echarts.init(document.getElementById("chart"));
  chart.setOption({
    color: ["#1d4ed8", "#0891b2", "#f59e0b"],
    tooltip: { trigger: "axis", valueFormatter: (value) => money.format(value) },
    legend: { data: ["Actual cash flow", "Forecast", "95% interval"] },
    grid: { left: 72, right: 36, top: 60, bottom: 90 },
    xAxis: { type: "category", data: dates, boundaryGap: false, axisLabel: { hideOverlap: true } },
    yAxis: { type: "value", axisLabel: { formatter: (value) => money.format(value) } },
    dataZoom: [{ type: "inside" }, { type: "slider", bottom: 18 }],
    series: [
      {
        name: "Actual cash flow", type: "line", smooth: true, symbolSize: 5,
        data: dates.map((date) => actualByDate.get(date) ?? null),
        markLine: anomalyDates.length ? { symbol: "none", lineStyle: { color: "#dc2626", type: "dotted" }, data: anomalyDates.map((date) => ({ xAxis: date })) } : undefined,
      },
      { name: "Forecast", type: "line", smooth: true, symbolSize: 5, lineStyle: { type: "dashed", width: 2 }, data: dates.map((date) => forecastByDate.get(date) ?? null) },
      { name: "Confidence base", type: "line", stack: "interval", symbol: "none", lineStyle: { opacity: 0 }, data: [...Array(actual.length).fill(null), ...lower] },
      { name: "95% interval", type: "line", stack: "interval", symbol: "none", lineStyle: { opacity: 0 }, areaStyle: { color: "#93c5fd", opacity: 0.35 }, data: [...Array(actual.length).fill(null), ...band] },
    ],
  });
  window.addEventListener("resize", () => chart.resize());

  document.getElementById("anomalies").innerHTML = anomalies.map((item) => `
    <tr>
      <td>${item.date}</td>
      <td><span class="tag ${item.type}">${item.type.replace("_", " ")}</span></td>
      <td>${money.format(item.amount)}</td>
      <td>${item.description || "-"}</td>
      <td>${item.reason}</td>
    </tr>`).join("") || "<tr><td colspan=\"5\">No findings.</td></tr>";
}

fetch("analytics-result.json")
  .then((response) => {
    if (!response.ok) throw new Error(`Could not load analytics-result.json (${response.status})`);
    return response.json();
  })
  .then(render)
  .catch((error) => showError(`${error.message}. Generate the file with the command in the demo instructions, then refresh.`));