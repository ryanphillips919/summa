import { rocket } from "https://cdn.jsdelivr.net/gh/starfederation/datastar@v1.0.4/bundles/datastar-rocket.js";

const echarts = await import(
  "https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.esm.js",
);

export { echarts };

rocket("echarts-view", {
  renderOnPropChange: false,

  props: ({ js, string, number }) => ({
    option: js,
    theme: string.default("default"),
    resizeDelay: number.min(0).default(100),
  }),

  onFirstRender: ({ refs, cleanup, observeProps, props }) => {
    let chart;
    let observer;
    let resizeTimer = 0;

    const update = () => {
      chart?.setOption(props.option ?? {}, true);
    };

    const resize = () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(
        () => chart?.resize(),
        props.resizeDelay,
      );
    };

    const initialize = () => {
      if (!(refs.container instanceof HTMLElement)) {
        return;
      }

      chart?.dispose();

      chart = echarts.init(
        refs.container,
        props.theme === "default" ? undefined : props.theme,
      );

      update();

      observer?.disconnect();
      observer = new ResizeObserver(resize);
      observer.observe(refs.container);
    };

    initialize();
    observeProps(update, "option");
    observeProps(initialize, "theme");
    observeProps(resize, "resizeDelay");

    cleanup(() => {
      observer?.disconnect();
      clearTimeout(resizeTimer);
      chart?.dispose();
    });
  },

  render: ({ html }) => html`
    <style>
      :host {
        display: block;
        height: 100%;
        min-height: inherit;
        width: 100%;
      }

      .chart-container {
        height: 100%;
        min-height: inherit;
        width: 100%;
      }
    </style>

    <div class="chart-container" data-ref:container></div>
  `,
});