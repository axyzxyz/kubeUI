import { http } from './http';
import type { EventItem, EventListQuery, PageResult } from './types';

export function listEvents(
  cluster: string,
  query: EventListQuery,
): Promise<PageResult<EventItem>> {
  return http.get<PageResult<EventItem>>(`/clusters/${cluster}/events`, {
    namespace: query.namespace,
    fieldSelector: query.fieldSelector,
    page: query.page,
    size: query.size,
    sortBy: query.sortBy,
    order: query.order,
  });
}
