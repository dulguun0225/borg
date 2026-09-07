import { bootstrapApplication } from '@angular/platform-browser';
import { Shell } from './app/app';
import { appConfig } from './app/app.config';

void bootstrapApplication(Shell, appConfig).catch((err: unknown) => {
  // Nothing has rendered at this point, so the console is the only place a
  // bootstrap failure can be reported.
  console.error(err);
});
