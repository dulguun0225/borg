// The one runner configuration. ChromeHeadlessNoSandbox is the launcher every
// run uses: the four screen predicates are decided over resolved styles and a
// real layout, which only a browser answers, and the sandbox is off because a
// container image and a hosted runner both refuse to enter it.
module.exports = function (config) {
  config.set({
    basePath: '',
    frameworks: ['jasmine'],
    plugins: [require('karma-jasmine'), require('karma-chrome-launcher')],
    reporters: ['progress'],
    browsers: ['ChromeHeadlessNoSandbox'],
    customLaunchers: {
      ChromeHeadlessNoSandbox: {
        base: 'ChromeHeadless',
        flags: ['--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage', '--window-size=1280,900'],
      },
    },
    restartOnFileChange: true,
    singleRun: false,
  });
};
