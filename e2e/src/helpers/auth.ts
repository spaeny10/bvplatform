import * as fs from 'fs';
import * as path from 'path';

/** Base URL shared by API request contexts created outside the page fixture. */
export const BASE_URL = process.env.IRONSIGHT_BASE_URL || 'http://127.0.0.1:13000';

export type RoleName = 'admin' | 'supervisor' | 'operator' | 'manager' | 'customer';

export interface RoleDef {
    role: RoleName;
    username: string;
    passwordEnv: 'IRONSIGHT_ADMIN_PASSWORD' | 'IRONSIGHT_DEMO_PASSWORD';
    /** required roles hard-fail the setup project; others soft-skip on 401. */
    required: boolean;
}

// Usernames come from the demo seed (cmd seed --all on bob). admin always
// exists; the other four only exist after the seed has been run.
export const ROLES: RoleDef[] = [
    { role: 'admin',      username: 'admin',       passwordEnv: 'IRONSIGHT_ADMIN_PASSWORD', required: true },
    { role: 'supervisor', username: 'rmorgan',     passwordEnv: 'IRONSIGHT_DEMO_PASSWORD',  required: false },
    { role: 'operator',   username: 'jhayes',      passwordEnv: 'IRONSIGHT_DEMO_PASSWORD',  required: false },
    { role: 'manager',    username: 'marcus.webb', passwordEnv: 'IRONSIGHT_DEMO_PASSWORD',  required: false },
    { role: 'customer',   username: 'spierce',     passwordEnv: 'IRONSIGHT_DEMO_PASSWORD',  required: false },
];

/** Path of the storageState file written by the setup project for a role. */
export function authFile(role: RoleName): string {
    return path.resolve(__dirname, '..', '..', '.auth', `${role}.json`);
}

/** True once the setup project produced a storage state for the role. */
export function hasAuthState(role: RoleName): boolean {
    return fs.existsSync(authFile(role));
}

export function rolePassword(def: RoleDef): string {
    if (def.passwordEnv === 'IRONSIGHT_ADMIN_PASSWORD') {
        const pw = process.env.IRONSIGHT_ADMIN_PASSWORD;
        if (!pw) {
            throw new Error(
                'IRONSIGHT_ADMIN_PASSWORD is not set. Retrieve it from the bob test host '
                + "(ssh fred, then: ssh jetstream@192.168.103.48 'sudo grep ADMIN_PASSWORD /etc/ironsight-test/db.env') "
                + 'and put it in e2e/.env.',
            );
        }
        return pw;
    }
    return process.env.IRONSIGHT_DEMO_PASSWORD || 'demo123';
}
