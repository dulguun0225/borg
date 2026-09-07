import { Routes } from '@angular/router';
import { workRoutes } from './work/work.routes';
import { opsRoutes } from './ops/ops.routes';
import { factoryRoutes } from './factory/factory.routes';
import { peopleRoutes } from './people/people.routes';

// The four screens and every address under them. Each screen owns its own
// addresses, and the shell knows nothing of a screen but its routes — which
// is why ../../../screens/server.go serves index.html for these four roots
// and everything beneath them.
export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'work' },
  { path: 'work', children: workRoutes },
  { path: 'ops', children: opsRoutes },
  { path: 'factory', children: factoryRoutes },
  { path: 'people', children: peopleRoutes },
  { path: '**', redirectTo: 'work' },
];
