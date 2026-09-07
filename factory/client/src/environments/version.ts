// The factory version this client was built from, carried on every call as
// X-Factory-Version. It must equal factoryVersion in ../../../cmd/factory/main.go;
// `npm run set-version` copies that constant here, and this one file is the
// whole of what a script writes into the source tree.
export const FACTORY_VERSION = 'unstamped';
