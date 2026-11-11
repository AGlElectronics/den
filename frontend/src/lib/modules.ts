import type { Module } from '@/api/modules';
import type { ServerDatabase } from '@/api/operations';

/**
 * Den only offers to delete a module's database when Den created it, the
 * database exists and is not protected, and the person has the ops role.
 */
export function canDropDatabase(m: Module, database: ServerDatabase | undefined, ops: boolean): boolean {
  return ops && m.database_created_by_den && !!database && !database.protected;
}
