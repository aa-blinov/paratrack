# echarts slim bundle

`internal/web/static/js/echarts.min.js` is NOT the full ECharts dist.
It is a tree-shaken build with only what paratrack's graph uses:

    BarChart + GridComponent + TooltipComponent + CanvasRenderer

Rebuild (needs npm):

    cd /tmp && rm -rf echarts-slim && mkdir echarts-slim && cd echarts-slim
    npm init -y >/dev/null
    npm install echarts@5.5.1 esbuild@0.25.0
    cat > entry.js << 'JS'
    import * as echarts from 'echarts/core';
    import { BarChart } from 'echarts/charts';
    import { GridComponent, TooltipComponent } from 'echarts/components';
    import { CanvasRenderer } from 'echarts/renderers';
    echarts.use([BarChart, GridComponent, TooltipComponent, CanvasRenderer]);
    window.echarts = echarts;
    JS
    npx esbuild entry.js --bundle --minify --format=iife \
      --outfile=echarts.slim.js --legal-comments=none \
      --define:__DEV__=false --drop:console --drop:debugger
    cp echarts.slim.js /path/to/paratrack/internal/web/static/js/echarts.min.js

~158 KB gzip vs ~334 KB for the stock `echarts.min.js`.
