import { FACTORY_VERSION } from '../../environments/version';

// The two headers every call carries, named the way
// ../../../../screens/version.go names them.
export const HEADER_VERSION = 'X-Factory-Version';
export const HEADER_PRINCIPAL = 'X-Factory-Principal';

// The factory version this client was built from. The server refuses a call
// whose version is not its own, on every call and not on the load alone, so
// this value is read on every request.
export const factoryVersion = (): string => FACTORY_VERSION;
