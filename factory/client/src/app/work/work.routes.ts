import { Routes } from '@angular/router';
import { WorkScreen } from './work';
import { BoardScreen } from './board';
import { ItemScreen } from './item';
import { DecisionScreen } from './decision';
import { IntakeScreen } from './intake';

// Every address of the Work screen. An item and a decision are two of the four
// places a human can be on the four screens, so a row the notifier delivers
// opens here and not at the home view.
export const workRoutes: Routes = [
  { path: '', component: WorkScreen },
  { path: 'all', component: BoardScreen },
  { path: 'intake', component: IntakeScreen },
  { path: 'item/:id', component: ItemScreen },
  { path: 'decision/:id', component: DecisionScreen },
];
