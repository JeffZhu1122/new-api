/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// The page the H5 link returns to; it reports the result from the phone.
export const VERIFICATION_CALLBACK_PATH = '/asset-library-verification'

// The H5 page reads its language from the lng parameter: zh, en or zh-Hant.
// Only that parameter is rewritten so the signed rest of the link is kept
// byte for byte.
export function localizedVerificationLink(link: string, language: string) {
  let lng = 'en'
  if (language === 'zhTW') {
    lng = 'zh-Hant'
  } else if (language.startsWith('zh')) {
    lng = 'zh'
  }
  if (/[?&]lng=[^&#]*/.test(link)) {
    return link.replace(/([?&])lng=[^&#]*/, `$1lng=${lng}`)
  }
  return `${link}${link.includes('?') ? '&' : '?'}lng=${lng}`
}
