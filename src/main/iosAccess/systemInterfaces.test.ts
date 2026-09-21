import { describe, expect, it, vi } from 'vitest'

const networkInterfaces = vi.fn()

vi.mock('node:os', () => ({
  networkInterfaces: () => networkInterfaces(),
}))

import { systemRawInterfaces } from './systemInterfaces'

describe('systemRawInterfaces', () => {
  it('maps each named interface to its address list, in os.networkInterfaces() order', () => {
    networkInterfaces.mockReturnValue({
      en0: [
        { address: '192.168.1.5', family: 'IPv4' },
        { address: 'fe80::1', family: 'IPv6' },
      ],
      lo0: [{ address: '127.0.0.1', family: 'IPv4' }],
    })

    expect(systemRawInterfaces()).toEqual([
      { name: 'en0', addresses: ['192.168.1.5', 'fe80::1'] },
      { name: 'lo0', addresses: ['127.0.0.1'] },
    ])
  })

  it('skips an interface name Node reports with no addresses', () => {
    networkInterfaces.mockReturnValue({
      en0: [{ address: '10.0.0.2', family: 'IPv4' }],
      en1: undefined,
      en2: [],
    })

    expect(systemRawInterfaces()).toEqual([{ name: 'en0', addresses: ['10.0.0.2'] }])
  })
})
