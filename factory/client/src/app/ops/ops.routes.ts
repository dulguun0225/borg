import { Routes } from '@angular/router';
import { OpsScreen } from './ops';
import { ServiceScreen } from './service';

// Every address of the Ops screen: the board, and a service on an
// environment, which is one of the four places a human can be on the four
// screens.
export const opsRoutes: Routes = [
  { path: '', component: OpsScreen },
  { path: 'service/:serviceId/on/:environmentId', component: ServiceScreen },
];
