const money = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

const categories = {
  aws: "Cloud services",
  "google ads": "Marketing",
  "freelance contractor": "Contractors",
  "city utilities": "Utilities",
  "office depot": "Office supplies",
  "office warehouse": "Office supplies",
  "travel hub": "Travel exception",
};

function categoryFor(vendor) {
  return categories[vendor.trim().toLowerCase()] || "Other operating expense";
}

function balanceData(actual, forecast, openingCashBalance) {
  let actualBalance = openingCashBalance;

  const actualBalances = actual.map((point) => {
    actualBalance += point.net_cash_flow;
    return { date: point.date, value: actualBalance };
  });

  let forecastBalance = actualBalance;

  const forecastBalances = forecast.map((point) => {
    forecastBalance += point.predicted_net_cash_flow;
    return { date: point.date, value: forecastBalance };
  });

  return {
    actualBalances,
    forecastBalances,
    lastActualBalance: actualBalance,
  };
}

export function buildCashChartOption(data, activeView) {
  const {
    daily_cash_flow: actual,
    forecast,
    anomalies,
    opening_cash_balance: openingCashBalance,
  } = data;

  const { actualBalances, forecastBalances, lastActualBalance } = balanceData(
    actual,
    forecast,
    openingCashBalance,
  );

  const actualByDate = new Map(
    actual.map((point) => [point.date, point.net_cash_flow]),
  );
  const forecastByDate = new Map(
    forecast.map((point) => [point.date, point.predicted_net_cash_flow]),
  );
  const actualBalanceByDate = new Map(
    actualBalances.map((point) => [point.date, point.value]),
  );
  const forecastBalanceByDate = new Map(
    forecastBalances.map((point) => [point.date, point.value]),
  );

  const dates = [
    ...actual.map((point) => point.date),
    ...forecast.map((point) => point.date),
  ];

  const lastActualDate = actual[actual.length - 1].date;
  const anomalyDates = [...new Set(anomalies.map((item) => item.date))];
  const balanceView = activeView === "balance";

  const actualLabel = balanceView
    ? "Actual cash balance"
    : "Actual cash flow";

  const forecastLabel = balanceView
    ? "Projected cash balance"
    : "Forecast cash flow";

  const actualSeries = balanceView
    ? dates.map((date) => actualBalanceByDate.get(date) ?? null)
    : dates.map((date) => actualByDate.get(date) ?? null);

  const forecastSeries = balanceView
    ? dates.map((date) => {
        if (date === lastActualDate) {
          return lastActualBalance;
        }

        return forecastBalanceByDate.get(date) ?? null;
      })
    : dates.map((date) => forecastByDate.get(date) ?? null);

  return {
    color: ["#1d4ed8", "#0891b2"],

    tooltip: {
      trigger: "axis",
      formatter(params) {
        const date = params[0]?.axisValue ?? "";
        const lines = [`<strong>${date}</strong>`];

        if (balanceView) {
          const actualBalance = actualBalanceByDate.get(date);
          const forecastBalance = forecastBalanceByDate.get(date);

          if (actualBalance != null) {
            lines.push(`Actual balance: ${money.format(actualBalance)}`);
          }

          if (forecastBalance != null) {
            lines.push(`Projected balance: ${money.format(forecastBalance)}`);
          }
        } else {
          const actualValue = actualByDate.get(date);
          const forecastValue = forecastByDate.get(date);

          if (actualValue != null) {
            lines.push(`Actual: ${money.format(actualValue)}`);
          }

          if (forecastValue != null) {
            lines.push(`Forecast: ${money.format(forecastValue)}`);
          }
        }

        return lines.join("<br>");
      },
    },

    legend: {
      data: [actualLabel, forecastLabel],
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
      axisLabel: { hideOverlap: true },
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
        name: actualLabel,
        type: "line",
        smooth: true,
        symbolSize: 5,
        data: actualSeries,

        markLine: anomalyDates.length
          ? {
              symbol: "none",
              lineStyle: {
                color: "#dc2626",
                type: "dotted",
              },
              data: anomalyDates.map((date) => ({ xAxis: date })),
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
        name: forecastLabel,
        type: "line",
        smooth: true,
        symbolSize: 5,
        lineStyle: {
          type: "dashed",
          width: 2,
        },
        data: forecastSeries,
      },
    ],
  };
}

export function buildSankey(data) {
  const cashIn = data
    .filter((movement) => movement.amount > 0)
    .reduce((total, movement) => total + movement.amount, 0);

  const spendingByCategory = new Map();

  data
    .filter((movement) => movement.amount < 0)
    .forEach((movement) => {
      const category = categoryFor(movement.vendor || "");
      const total = spendingByCategory.get(category) || 0;

      spendingByCategory.set(category, total + Math.abs(movement.amount));
    });

  const cashOut = [...spendingByCategory.values()].reduce(
    (total, amount) => total + amount,
    0,
  );

  const netCash = cashIn - cashOut;

  const links = [
    { source: "Customer receipts", target: "Cash available", value: cashIn },
    ...[...spendingByCategory.entries()].map(([category, value]) => ({
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

  return {
    cashIn,
    cashOut,
    netCash,

    option: {
      tooltip: {
        trigger: "item",
        valueFormatter: (value) => money.format(value),
      },

      series: [
        {
          type: "sankey",
          data: [...new Set(
            links.flatMap((link) => [link.source, link.target]),
          )].map((name) => ({ name })),
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
    },
  };
}