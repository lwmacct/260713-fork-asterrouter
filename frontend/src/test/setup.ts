import { afterEach, beforeEach } from 'vitest'

beforeEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  document.body.innerHTML = ''
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.setAttribute('lang', 'en-US')
})

afterEach(() => {
  document.body.innerHTML = ''
})
