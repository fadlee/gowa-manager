import { t } from 'elysia'
import type { System } from '../../types'

export namespace SystemModel {
  // System status response
  export const statusResponse = t.Object({
    status: t.String(),
    uptime: t.Number(),
    instances: t.Object({
      total: t.Number(),
      running: t.Number(),
      stopped: t.Number()
    }),
    ports: t.Object({
      allocated: t.Number(),
      next_available: t.Number()
    })
  })
  export type statusResponse = System.StatusResponse

  // Port info response
  export const portInfo = t.Object({
    port: t.Number(),
    is_allocated: t.Boolean(),
    instance_id: t.Union([t.Number(), t.Null()])
  })
  export type portInfo = System.PortInfo

  export const portsResponse = t.Array(portInfo)
  export type portsResponse = System.PortsResponse

  // Config response
  export const configResponse = t.Object({
    port_range: t.Object({
      min: t.Number(),
      max: t.Number()
    }),
    data_directory: t.String(),
    binaries_directory: t.String()
  })
  export type configResponse = System.ConfigResponse

  export const portAvailabilityResponse = t.Object({
    available: t.Boolean(),
    port: t.Union([t.Number(), t.Null()])
  })
  export type portAvailabilityResponse = System.PortAvailabilityResponse

  export const portCheckResponse = t.Object({
    port: t.Number(),
    available: t.Boolean()
  })
  export type portCheckResponse = System.PortCheckResponse
}
