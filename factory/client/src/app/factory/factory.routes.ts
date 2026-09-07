import { Routes } from '@angular/router';
import { FactoryScreen } from './factory';

// Every address of the Factory screen: itself, and nothing under it. A
// constraint's own address is the fourth place a human can be, and it is read
// at Work on the intent that carried it or here in the list of what is in
// force.
export const factoryRoutes: Routes = [{ path: '', component: FactoryScreen }];
