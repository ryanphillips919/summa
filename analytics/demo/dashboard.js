const money = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

function card(label, value) {
  return `
    <article class="card">
      <div class="label">${label}</div>
      <div class="value">${value}</div>
    </article>
  `;
}

function showError(message) {
  const error = document.getElementById("error");
  error.textContent = message;
  error.hidden = false;
}

function categoryFor(vendor) {
  const categories = {
    aws: "Cloud services",
    "google ads": "Marketing",
    "freelance contractor": "Contractors",
    "city utilities": "Utilities",
    "office depot": "Office supplies",
    "office warehouse": "Office supplies",
    "travel hub": "Travel exception",
  };

  return categories[vendor.trim().toLowerCase()] || "Other operating expense";
}

function renderSankey(movements) {
  const cashIn = movements
    .filter((movement) => movement.amount > 0)
    .reduce((total, movement) => total + movement.amount, 0);

  const byCategory = new Map();

  movements
    .filter((movement) => movement.amount < 0)
    .forEach((movement) => {
      const category = categoryFor(movement.vendor || "");
      const currentTotal = byCategory.get(category) || 0;

      byCategory.set(
        category,
        currentTotal + Math.abs(movement.amount)
      );
    });

  const cashOut = [...byCategory.values()]
    .reduce((total, amount) => total + amount, 0);

  const netCash = cashIn - cashOut;

  const links = [
    {
      source: "Customer receipts",
      target: "Cash available",
      value: cashIn,
    },
    ...[...byCategory.entries()].map(([category, value]) => ({
      source: "Cash available",
      target: category,
      value,
    })),
  ];

  if (netCash > 0) {
    links.push({
      source: "Cash available",
      target: "Net cash retained",
      value: netCash,
    });
  }

  document.getElementById("cash-summary").textContent =
    `The business received ${money.format(cashIn)}, spent ${money.format(cashOut)} on operations, and retained ${money.format(netCash)} in net cash.`;

  const sankey = echarts.init(document.getElementById("sankey"));

  sankey.setOption({
    tooltip: {
      trigger: "item",
      valueFormatter: (value) => money.format(value),
    },

    series: [
      {
        type: "sankey",

        data: [
          ...new Set(
            links.flatMap((link) => [link.source, link.target])
          ),
        ].map((name) => ({ name })),

        links,

        nodeAlign: "justify",
        layoutIterations: 32,

        label: {
          color: "#172033",
          fontWeight: 600,
        },

        lineStyle: {
          color: "gradient",
          curveness: 0.5,
          opacity: 0.45,
        },

        emphasis: {
          focus: "adjacency",
        },

        itemStyle: {
          borderColor: "#ffffff",
          borderWidth: 1,
        },
      },
    ],
  });

  window.addEventListener("resize", () => sankey.resize());
}

function render(result, movements) {
  const {
    summary,
    daily_cash_flow: actual,
    forecast,
    anomalies,
  } = result;

  document.getElementById("summary").innerHTML = [
    card("Forecast model", summary.forecast_model),

    card(
      "Baseline MAE",
      summary.baseline_validation_mae == null
        ? "Need more history"
        : money.format(summary.baseline_validation_mae)
    ),

    card(
      "Selected-model MAE",
      summary.selected_model_validation_mae == null
        ? "Need more history"
        : money.format(summary.selected_model_validation_mae)
    ),

    card("Items to review", summary.anomaly_count),
  ].join("");

  const actualByDate = new Map(
    actual.map((point) => [point.date, point.net_cash_flow])
  );

  const forecastByDate = new Map(
    forecast.map((point) => [point.date, point])
  );

  const dates = [
    ...actual.map((point) => point.date),
    ...forecast.map((point) => point.date),
  ];

  const anomalyDates = [
    ...new Set(anomalies.map((item) => item.date)),
  ];

  const chart = echarts.init(document.getElementById("chart"));

  chart.setOption({
    color: ["#1d4ed8", "#0891b2"],

    tooltip: {
      trigger: "axis",

      formatter(params) {
        const date = params[0]?.axisValue ?? "";
        const actualValue = actualByDate.get(date);
        const forecastPoint = forecastByDate.get(date);

        const lines = [`<strong>${date}</strong>`];

        if (actualValue != null) {
          lines.push(`Actual: ${money.format(actualValue)}`);
        }

        if (forecastPoint) {
          lines.push(
            `Forecast: ${money.format(
              forecastPoint.predicted_net_cash_flow
            )}`
          );
        }

        return lines.join("<br>");
      },
    },

    legend: {
      data: ["Actual cash flow", "Forecast"],
    },

    grid: {
      left: 72,
      right: 36,
      top: 60,
      bottom: 90,
    },

    xAxis: {
      type: "category",
      data: dates,
      boundaryGap: false,
      axisLabel: {
        hideOverlap: true,
      },
    },

    yAxis: {
      type: "value",
      axisLabel: {
        formatter: (value) => money.format(value),
      },
    },

    dataZoom: [
      { type: "inside" },
      { type: "slider", bottom: 18 },
    ],

    series: [
      {
        name: "Actual cash flow",
        type: "line",
        smooth: true,
        symbolSize: 5,

        data: dates.map(
          (date) => actualByDate.get(date) ?? null
        ),

        markLine: anomalyDates.length
          ? {
              symbol: "none",
              lineStyle: {
                color: "#dc2626",
                type: "dotted",
              },
              data: anomalyDates.map((date) => ({
                xAxis: date,
              })),
            }
          : undefined,

        markArea: forecast.length
          ? {
              silent: true,

              itemStyle: {
                color: "rgba(148, 163, 184, 0.10)",
              },

              label: {
                show: true,
                color: "#64748b",
                formatter: "Forecast period",
              },

              data: [[
                { xAxis: forecast[0].date },
                { xAxis: forecast[forecast.length - 1].date },
              ]],
            }
          : undefined,
      },

      {
        name: "Forecast",
        type: "line",
        smooth: true,
        symbolSize: 5,

        lineStyle: {
          type: "dashed",
          width: 2,
        },

        data: dates.map(
          (date) =>
            forecastByDate.get(date)?.predicted_net_cash_flow ?? null
        ),
      },
    ],
  });

  window.addEventListener("resize", () => chart.resize());

  document.getElementById("anomalies").innerHTML =
    anomalies.map((item) => `
      <tr>
        <td>${item.date}</td>
        <td>
          <span class="tag ${item.type}">
            ${item.type.replace("_", " ")}
          </span>
        </td>
        <td>${money.format(item.amount)}</td>
        <td>${item.description || "-"}</td>
        <td>${item.reason}</td>
      </tr>
    `).join("")
    || `<tr><td colspan="5">No findings.</td></tr>`;

  renderSankey(movements);
}

fetch("/api/analytics/demo")
  .then((response) => {
    if (!response.ok) {
      throw new Error(`Could not load Summa analytics (${response.status})`);
    }
    return response.json();
  })
  .then((payload) => render(payload, payload.cash_movements))
  .catch((error) => {
    showError(`${error.message}. Make sure the Summa server is running, then refresh.`);
  });