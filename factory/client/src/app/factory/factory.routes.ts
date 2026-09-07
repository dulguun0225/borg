import { Routes } from '@angular/router';
import { FactoryScreen } from './factory';
import { FactoryConstraintScreen } from './factory-constraint';

// Every address of the Factory screen: itself; the fleet and the constraints
// in force as two child paths, because dispatch's stop and a safeguard's
// route both name "/factory/fleet" and "/factory/constraints" as where a
// cause is lifted from (../../../../screens' liftedAt) and a screen has to
// answer both rather than fall through to the catch-all redirect; and a
// constraint's own address, "/factory/constraints/:id", the fourth place a
// human can be. The two child paths render the same screen as '', with the
// section named in `data` reaching it as the `section` input.
export const factoryRoutes: Routes = [
  { path: '', component: FactoryScreen },
  { path: 'fleet', component: FactoryScreen, data: { section: 'fleet' } },
  { path: 'constraints', component: FactoryScreen, data: { section: 'constraints' } },
  { path: 'constraints/:id', component: FactoryConstraintScreen },
];
