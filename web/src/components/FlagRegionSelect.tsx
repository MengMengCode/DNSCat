import React, { useState, useRef, useEffect } from 'react';
import { ChevronDown, Check, X, Search, Globe } from 'lucide-react';
import { useI18n } from '../i18n/I18nContext';

export interface RegionOption {
  code: string;
  flag: string;
  nameZh: string;
  nameEn: string;
  category: 'country' | 'global';
}

export const REGION_OPTIONS: RegionOption[] = [
  // Global & Regional Groups
  { code: 'default', flag: '🌐', nameZh: '全球默认 (Global Default)', nameEn: 'Global Default', category: 'global' },
  { code: 'eu', flag: '🇪🇺', nameZh: '欧洲联盟 / 欧洲地区 (Europe)', nameEn: 'European Union / Europe', category: 'global' },

  // East Asia
  { code: 'cn', flag: '🇨🇳', nameZh: '中国大陆 (Mainland China)', nameEn: 'Mainland China', category: 'country' },
  { code: 'hk', flag: '🇭🇰', nameZh: '中国香港 (Hong Kong)', nameEn: 'Hong Kong', category: 'country' },
  { code: 'mo', flag: '🇲🇴', nameZh: '中国澳门 (Macao)', nameEn: 'Macao', category: 'country' },
  { code: 'tw', flag: '🇹🇼', nameZh: '中国台湾 (Taiwan)', nameEn: 'Taiwan', category: 'country' },
  { code: 'jp', flag: '🇯🇵', nameZh: '日本 (Japan)', nameEn: 'Japan', category: 'country' },
  { code: 'kr', flag: '🇰🇷', nameZh: '韩国 (South Korea)', nameEn: 'South Korea', category: 'country' },
  { code: 'kp', flag: '🇰🇵', nameZh: '朝鲜 (North Korea)', nameEn: 'North Korea', category: 'country' },
  { code: 'mn', flag: '🇲🇳', nameZh: '蒙古 (Mongolia)', nameEn: 'Mongolia', category: 'country' },

  // Southeast Asia
  { code: 'sg', flag: '🇸🇬', nameZh: '新加坡 (Singapore)', nameEn: 'Singapore', category: 'country' },
  { code: 'my', flag: '🇲🇾', nameZh: '马来西亚 (Malaysia)', nameEn: 'Malaysia', category: 'country' },
  { code: 'th', flag: '🇹🇭', nameZh: '泰国 (Thailand)', nameEn: 'Thailand', category: 'country' },
  { code: 'vn', flag: '🇻🇳', nameZh: '越南 (Vietnam)', nameEn: 'Vietnam', category: 'country' },
  { code: 'id', flag: '🇮🇩', nameZh: '印度尼西亚 (Indonesia)', nameEn: 'Indonesia', category: 'country' },
  { code: 'ph', flag: '🇵🇭', nameZh: '菲律宾 (Philippines)', nameEn: 'Philippines', category: 'country' },
  { code: 'kh', flag: '🇰🇭', nameZh: '柬埔寨 (Cambodia)', nameEn: 'Cambodia', category: 'country' },
  { code: 'la', flag: '🇱🇦', nameZh: '老挝 (Laos)', nameEn: 'Laos', category: 'country' },
  { code: 'mm', flag: '🇲🇲', nameZh: '缅甸 (Myanmar)', nameEn: 'Myanmar', category: 'country' },
  { code: 'bn', flag: '🇧🇳', nameZh: '文莱 (Brunei)', nameEn: 'Brunei', category: 'country' },
  { code: 'tl', flag: '🇹🇱', nameZh: '东帝汶 (Timor-Leste)', nameEn: 'Timor-Leste', category: 'country' },

  // South Asia
  { code: 'in', flag: '🇮🇳', nameZh: '印度 (India)', nameEn: 'India', category: 'country' },
  { code: 'pk', flag: '🇵🇰', nameZh: '巴基斯坦 (Pakistan)', nameEn: 'Pakistan', category: 'country' },
  { code: 'bd', flag: '🇧🇩', nameZh: '孟加拉国 (Bangladesh)', nameEn: 'Bangladesh', category: 'country' },
  { code: 'lk', flag: '🇱🇰', nameZh: '斯里兰卡 (Sri Lanka)', nameEn: 'Sri Lanka', category: 'country' },
  { code: 'np', flag: '🇳🇵', nameZh: '尼泊尔 (Nepal)', nameEn: 'Nepal', category: 'country' },
  { code: 'bt', flag: '🇧🇹', nameZh: '不丹 (Bhutan)', nameEn: 'Bhutan', category: 'country' },
  { code: 'mv', flag: '🇲🇻', nameZh: '马尔代夫 (Maldives)', nameEn: 'Maldives', category: 'country' },

  // Central Asia
  { code: 'kz', flag: '🇰🇿', nameZh: '哈萨克斯坦 (Kazakhstan)', nameEn: 'Kazakhstan', category: 'country' },
  { code: 'uz', flag: '🇺🇿', nameZh: '乌兹别克斯坦 (Uzbekistan)', nameEn: 'Uzbekistan', category: 'country' },
  { code: 'tm', flag: '🇹🇲', nameZh: '土库曼斯坦 (Turkmenistan)', nameEn: 'Turkmenistan', category: 'country' },
  { code: 'kg', flag: '🇰🇬', nameZh: '吉尔吉斯斯坦 (Kyrgyzstan)', nameEn: 'Kyrgyzstan', category: 'country' },
  { code: 'tj', flag: '🇹🇯', nameZh: '塔吉克斯坦 (Tajikistan)', nameEn: 'Tajikistan', category: 'country' },
  { code: 'af', flag: '🇦🇫', nameZh: '阿富汗 (Afghanistan)', nameEn: 'Afghanistan', category: 'country' },

  // Middle East & West Asia
  { code: 'ae', flag: '🇦🇪', nameZh: '阿联酋 (United Arab Emirates)', nameEn: 'United Arab Emirates', category: 'country' },
  { code: 'sa', flag: '🇸🇦', nameZh: '沙特阿拉伯 (Saudi Arabia)', nameEn: 'Saudi Arabia', category: 'country' },
  { code: 'qa', flag: '🇶🇦', nameZh: '卡塔尔 (Qatar)', nameEn: 'Qatar', category: 'country' },
  { code: 'kw', flag: '🇰🇼', nameZh: '科威特 (Kuwait)', nameEn: 'Kuwait', category: 'country' },
  { code: 'om', flag: '🇴🇲', nameZh: '阿曼 (Oman)', nameEn: 'Oman', category: 'country' },
  { code: 'bh', flag: '🇧🇭', nameZh: '巴林 (Bahrain)', nameEn: 'Bahrain', category: 'country' },
  { code: 'il', flag: '🇮🇱', nameZh: '以色列 (Israel)', nameEn: 'Israel', category: 'country' },
  { code: 'tr', flag: '🇹🇷', nameZh: '土耳其 (Turkey)', nameEn: 'Turkey', category: 'country' },
  { code: 'ir', flag: '🇮🇷', nameZh: '伊朗 (Iran)', nameEn: 'Iran', category: 'country' },
  { code: 'iq', flag: '🇮🇶', nameZh: '伊拉克 (Iraq)', nameEn: 'Iraq', category: 'country' },
  { code: 'sy', flag: '🇸🇾', nameZh: '叙利亚 (Syria)', nameEn: 'Syria', category: 'country' },
  { code: 'jo', flag: '🇯🇴', nameZh: '约旦 (Jordan)', nameEn: 'Jordan', category: 'country' },
  { code: 'lb', flag: '🇱🇧', nameZh: '黎巴嫩 (Lebanon)', nameEn: 'Lebanon', category: 'country' },
  { code: 'ps', flag: '🇵🇸', nameZh: '巴勒斯坦 (Palestine)', nameEn: 'Palestine', category: 'country' },
  { code: 'ye', flag: '🇾🇪', nameZh: '也门 (Yemen)', nameEn: 'Yemen', category: 'country' },
  { code: 'am', flag: '🇦🇲', nameZh: '亚美尼亚 (Armenia)', nameEn: 'Armenia', category: 'country' },
  { code: 'az', flag: '🇦🇿', nameZh: '阿塞拜疆 (Azerbaijan)', nameEn: 'Azerbaijan', category: 'country' },
  { code: 'ge', flag: '🇬🇪', nameZh: '格鲁吉亚 (Georgia)', nameEn: 'Georgia', category: 'country' },
  { code: 'cy', flag: '🇨🇾', nameZh: '塞浦路斯 (Cyprus)', nameEn: 'Cyprus', category: 'country' },

  // North America
  { code: 'us', flag: '🇺🇸', nameZh: '美国 (United States)', nameEn: 'United States', category: 'country' },
  { code: 'ca', flag: '🇨🇦', nameZh: '加拿大 (Canada)', nameEn: 'Canada', category: 'country' },
  { code: 'mx', flag: '🇲🇽', nameZh: '墨西哥 (Mexico)', nameEn: 'Mexico', category: 'country' },
  { code: 'cu', flag: '🇨🇺', nameZh: '古巴 (Cuba)', nameEn: 'Cuba', category: 'country' },
  { code: 'do', flag: '🇩🇴', nameZh: '多米尼加 (Dominican Republic)', nameEn: 'Dominican Republic', category: 'country' },
  { code: 'ht', flag: '🇭🇹', nameZh: '海地 (Haiti)', nameEn: 'Haiti', category: 'country' },
  { code: 'jm', flag: '🇯🇲', nameZh: '牙买加 (Jamaica)', nameEn: 'Jamaica', category: 'country' },
  { code: 'pr', flag: '🇵🇷', nameZh: '波多黎各 (Puerto Rico)', nameEn: 'Puerto Rico', category: 'country' },
  { code: 'cr', flag: '🇨🇷', nameZh: '哥斯达黎加 (Costa Rica)', nameEn: 'Costa Rica', category: 'country' },
  { code: 'pa', flag: '🇵🇦', nameZh: '巴拿马 (Panama)', nameEn: 'Panama', category: 'country' },
  { code: 'gt', flag: '🇬🇹', nameZh: '危地马拉 (Guatemala)', nameEn: 'Guatemala', category: 'country' },
  { code: 'hn', flag: '🇭🇳', nameZh: '洪都拉斯 (Honduras)', nameEn: 'Honduras', category: 'country' },
  { code: 'sv', flag: '🇸🇻', nameZh: '萨尔瓦多 (El Salvador)', nameEn: 'El Salvador', category: 'country' },
  { code: 'ni', flag: '🇳🇮', nameZh: '尼加拉瓜 (Nicaragua)', nameEn: 'Nicaragua', category: 'country' },
  { code: 'bz', flag: '🇧🇿', nameZh: '伯利兹 (Belize)', nameEn: 'Belize', category: 'country' },
  { code: 'bs', flag: '🇧🇸', nameZh: '巴哈马 (Bahamas)', nameEn: 'Bahamas', category: 'country' },
  { code: 'tt', flag: '🇹🇹', nameZh: '特立尼达和多巴哥 (Trinidad and Tobago)', nameEn: 'Trinidad and Tobago', category: 'country' },
  { code: 'bb', flag: '🇧🇧', nameZh: '巴巴多斯 (Barbados)', nameEn: 'Barbados', category: 'country' },
  { code: 'lc', flag: '🇱🇨', nameZh: '圣卢西亚 (Saint Lucia)', nameEn: 'Saint Lucia', category: 'country' },
  { code: 'gd', flag: '🇬🇩', nameZh: '格林纳达 (Grenada)', nameEn: 'Grenada', category: 'country' },
  { code: 'bm', flag: '🇧🇲', nameZh: '百慕大 (Bermuda)', nameEn: 'Bermuda', category: 'country' },
  { code: 'ky', flag: '🇰🇾', nameZh: '开曼群岛 (Cayman Islands)', nameEn: 'Cayman Islands', category: 'country' },

  // South America
  { code: 'br', flag: '🇧🇷', nameZh: '巴西 (Brazil)', nameEn: 'Brazil', category: 'country' },
  { code: 'ar', flag: '🇦🇷', nameZh: '阿根廷 (Argentina)', nameEn: 'Argentina', category: 'country' },
  { code: 'cl', flag: '🇨🇱', nameZh: '智利 (Chile)', nameEn: 'Chile', category: 'country' },
  { code: 'co', flag: '🇨🇴', nameZh: '哥伦比亚 (Colombia)', nameEn: 'Colombia', category: 'country' },
  { code: 'pe', flag: '🇵🇪', nameZh: '秘鲁 (Peru)', nameEn: 'Peru', category: 'country' },
  { code: 've', flag: '🇻🇪', nameZh: '委内瑞拉 (Venezuela)', nameEn: 'Venezuela', category: 'country' },
  { code: 'ec', flag: '🇪🇨', nameZh: '厄瓜多尔 (Ecuador)', nameEn: 'Ecuador', category: 'country' },
  { code: 'bo', flag: '🇧🇴', nameZh: '玻利维亚 (Bolivia)', nameEn: 'Bolivia', category: 'country' },
  { code: 'py', flag: '🇵🇾', nameZh: '巴拉圭 (Paraguay)', nameEn: 'Paraguay', category: 'country' },
  { code: 'uy', flag: '🇺🇾', nameZh: '乌拉圭 (Uruguay)', nameEn: 'Uruguay', category: 'country' },
  { code: 'gy', flag: '🇬🇾', nameZh: '圭亚那 (Guyana)', nameEn: 'Guyana', category: 'country' },
  { code: 'sr', flag: '🇸🇷', nameZh: '苏里南 (Suriname)', nameEn: 'Suriname', category: 'country' },

  // Europe
  { code: 'de', flag: '🇩🇪', nameZh: '德国 (Germany)', nameEn: 'Germany', category: 'country' },
  { code: 'gb', flag: '🇬🇧', nameZh: '英国 (United Kingdom)', nameEn: 'United Kingdom', category: 'country' },
  { code: 'fr', flag: '🇫🇷', nameZh: '法国 (France)', nameEn: 'France', category: 'country' },
  { code: 'it', flag: '🇮🇹', nameZh: '意大利 (Italy)', nameEn: 'Italy', category: 'country' },
  { code: 'es', flag: '🇪🇸', nameZh: '西班牙 (Spain)', nameEn: 'Spain', category: 'country' },
  { code: 'nl', flag: '🇳🇱', nameZh: '荷兰 (Netherlands)', nameEn: 'Netherlands', category: 'country' },
  { code: 'pl', flag: '🇵🇱', nameZh: '波兰 (Poland)', nameEn: 'Poland', category: 'country' },
  { code: 'se', flag: '🇸🇪', nameZh: '瑞典 (Sweden)', nameEn: 'Sweden', category: 'country' },
  { code: 'ch', flag: '🇨🇭', nameZh: '瑞士 (Switzerland)', nameEn: 'Switzerland', category: 'country' },
  { code: 'no', flag: '🇳🇴', nameZh: '挪威 (Norway)', nameEn: 'Norway', category: 'country' },
  { code: 'fi', flag: '🇫🇮', nameZh: '芬兰 (Finland)', nameEn: 'Finland', category: 'country' },
  { code: 'dk', flag: '🇩🇰', nameZh: '丹麦 (Denmark)', nameEn: 'Denmark', category: 'country' },
  { code: 'be', flag: '🇧🇪', nameZh: '比利时 (Belgium)', nameEn: 'Belgium', category: 'country' },
  { code: 'at', flag: '🇦🇹', nameZh: '奥地利 (Austria)', nameEn: 'Austria', category: 'country' },
  { code: 'pt', flag: '🇵🇹', nameZh: '葡萄牙 (Portugal)', nameEn: 'Portugal', category: 'country' },
  { code: 'gr', flag: '🇬🇷', nameZh: '希腊 (Greece)', nameEn: 'Greece', category: 'country' },
  { code: 'cz', flag: '🇨🇿', nameZh: '捷克 (Czechia)', nameEn: 'Czechia', category: 'country' },
  { code: 'ro', flag: '🇷🇴', nameZh: '罗马尼亚 (Romania)', nameEn: 'Romania', category: 'country' },
  { code: 'hu', flag: '🇭🇺', nameZh: '匈牙利 (Hungary)', nameEn: 'Hungary', category: 'country' },
  { code: 'ie', flag: '🇮🇪', nameZh: '爱尔兰 (Ireland)', nameEn: 'Ireland', category: 'country' },
  { code: 'ua', flag: '🇺🇦', nameZh: '乌克兰 (Ukraine)', nameEn: 'Ukraine', category: 'country' },
  { code: 'ru', flag: '🇷🇺', nameZh: '俄罗斯 (Russia)', nameEn: 'Russia', category: 'country' },
  { code: 'by', flag: '🇧🇾', nameZh: '白俄罗斯 (Belarus)', nameEn: 'Belarus', category: 'country' },
  { code: 'bg', flag: '🇧🇬', nameZh: '保加利亚 (Bulgaria)', nameEn: 'Bulgaria', category: 'country' },
  { code: 'hr', flag: '🇭🇷', nameZh: '克罗地亚 (Croatia)', nameEn: 'Croatia', category: 'country' },
  { code: 'sk', flag: '🇸🇰', nameZh: '斯洛伐克 (Slovakia)', nameEn: 'Slovakia', category: 'country' },
  { code: 'si', flag: '🇸🇮', nameZh: '斯洛文尼亚 (Slovenia)', nameEn: 'Slovenia', category: 'country' },
  { code: 'lt', flag: '🇱🇹', nameZh: '立陶宛 (Lithuania)', nameEn: 'Lithuania', category: 'country' },
  { code: 'lv', flag: '🇱🇻', nameZh: '拉脱维亚 (Latvia)', nameEn: 'Latvia', category: 'country' },
  { code: 'ee', flag: '🇪🇪', nameZh: '爱沙尼亚 (Estonia)', nameEn: 'Estonia', category: 'country' },
  { code: 'is', flag: '🇮🇸', nameZh: '冰岛 (Iceland)', nameEn: 'Iceland', category: 'country' },
  { code: 'al', flag: '🇦🇱', nameZh: '阿尔巴尼亚 (Albania)', nameEn: 'Albania', category: 'country' },
  { code: 'rs', flag: '🇷🇸', nameZh: '塞尔维亚 (Serbia)', nameEn: 'Serbia', category: 'country' },
  { code: 'ba', flag: '🇧🇦', nameZh: '波黑 (Bosnia and Herzegovina)', nameEn: 'Bosnia and Herzegovina', category: 'country' },
  { code: 'me', flag: '🇲🇪', nameZh: '黑山 (Montenegro)', nameEn: 'Montenegro', category: 'country' },
  { code: 'mk', flag: '🇲🇰', nameZh: '北马其顿 (North Macedonia)', nameEn: 'North Macedonia', category: 'country' },
  { code: 'md', flag: '🇲🇩', nameZh: '摩尔多瓦 (Moldova)', nameEn: 'Moldova', category: 'country' },
  { code: 'lu', flag: '🇱🇺', nameZh: '卢森堡 (Luxembourg)', nameEn: 'Luxembourg', category: 'country' },
  { code: 'mt', flag: '🇲🇹', nameZh: '马耳他 (Malta)', nameEn: 'Malta', category: 'country' },
  { code: 'mc', flag: '🇲🇨', nameZh: '摩纳哥 (Monaco)', nameEn: 'Monaco', category: 'country' },
  { code: 'li', flag: '🇱🇮', nameZh: '列支敦士登 (Liechtenstein)', nameEn: 'Liechtenstein', category: 'country' },
  { code: 'ad', flag: '🇦🇩', nameZh: '安道尔 (Andorra)', nameEn: 'Andorra', category: 'country' },
  { code: 'sm', flag: '🇸🇲', nameZh: '圣马力诺 (San Marino)', nameEn: 'San Marino', category: 'country' },
  { code: 'va', flag: '🇻🇦', nameZh: '梵蒂冈 (Vatican City)', nameEn: 'Vatican City', category: 'country' },

  // Oceania
  { code: 'au', flag: '🇦🇺', nameZh: '澳大利亚 (Australia)', nameEn: 'Australia', category: 'country' },
  { code: 'nz', flag: '🇳🇿', nameZh: '新西兰 (New Zealand)', nameEn: 'New Zealand', category: 'country' },
  { code: 'pg', flag: '🇵🇬', nameZh: '巴布亚新几内亚 (Papua New Guinea)', nameEn: 'Papua New Guinea', category: 'country' },
  { code: 'fj', flag: '🇫🇯', nameZh: '斐济 (Fiji)', nameEn: 'Fiji', category: 'country' },
  { code: 'sb', flag: '🇸🇧', nameZh: '所罗门群岛 (Solomon Islands)', nameEn: 'Solomon Islands', category: 'country' },
  { code: 'vu', flag: '🇻🇺', nameZh: '瓦努阿图 (Vanuatu)', nameEn: 'Vanuatu', category: 'country' },
  { code: 'ws', flag: '🇼🇸', nameZh: '萨摩亚 (Samoa)', nameEn: 'Samoa', category: 'country' },
  { code: 'to', flag: '🇹🇴', nameZh: '汤加 (Tonga)', nameEn: 'Tonga', category: 'country' },
  { code: 'fm', flag: '🇫🇲', nameZh: '密克罗尼西亚 (Micronesia)', nameEn: 'Micronesia', category: 'country' },
  { code: 'pw', flag: '🇵🇼', nameZh: '帕劳 (Palau)', nameEn: 'Palau', category: 'country' },
  { code: 'gu', flag: '🇬🇺', nameZh: '关岛 (Guam)', nameEn: 'Guam', category: 'country' },

  // Africa
  { code: 'za', flag: '🇿🇦', nameZh: '南非 (South Africa)', nameEn: 'South Africa', category: 'country' },
  { code: 'eg', flag: '🇪🇬', nameZh: '埃及 (Egypt)', nameEn: 'Egypt', category: 'country' },
  { code: 'ng', flag: '🇳🇬', nameZh: '尼日利亚 (Nigeria)', nameEn: 'Nigeria', category: 'country' },
  { code: 'ke', flag: '🇰🇪', nameZh: '肯尼亚 (Kenya)', nameEn: 'Kenya', category: 'country' },
  { code: 'et', flag: '🇪🇹', nameZh: '埃塞俄比亚 (Ethiopia)', nameEn: 'Ethiopia', category: 'country' },
  { code: 'ma', flag: '🇲🇦', nameZh: '摩洛哥 (Morocco)', nameEn: 'Morocco', category: 'country' },
  { code: 'dz', flag: '🇩🇿', nameZh: '阿尔及利亚 (Algeria)', nameEn: 'Algeria', category: 'country' },
  { code: 'tz', flag: '🇹🇿', nameZh: '坦桑尼亚 (Tanzania)', nameEn: 'Tanzania', category: 'country' },
  { code: 'gh', flag: '🇬🇭', nameZh: '加纳 (Ghana)', nameEn: 'Ghana', category: 'country' },
  { code: 'sd', flag: '🇸🇩', nameZh: '苏丹 (Sudan)', nameEn: 'Sudan', category: 'country' },
  { code: 'ug', flag: '🇺🇬', nameZh: '乌干达 (Uganda)', nameEn: 'Uganda', category: 'country' },
  { code: 'ao', flag: '🇦🇴', nameZh: '安哥拉 (Angola)', nameEn: 'Angola', category: 'country' },
  { code: 'mz', flag: '🇲🇿', nameZh: '莫桑比克 (Mozambique)', nameEn: 'Mozambique', category: 'country' },
  { code: 'ci', flag: '🇨🇮', nameZh: '科特迪瓦 (Côte d\'Ivoire)', nameEn: 'Côte d\'Ivoire', category: 'country' },
  { code: 'cm', flag: '🇨🇲', nameZh: '喀麦隆 (Cameroon)', nameEn: 'Cameroon', category: 'country' },
  { code: 'mg', flag: '🇲🇬', nameZh: '马达加斯加 (Madagascar)', nameEn: 'Madagascar', category: 'country' },
  { code: 'zw', flag: '🇿🇼', nameZh: '津巴布韦 (Zimbabwe)', nameEn: 'Zimbabwe', category: 'country' },
  { code: 'sn', flag: '🇸🇳', nameZh: '塞内加尔 (Senegal)', nameEn: 'Senegal', category: 'country' },
  { code: 'zm', flag: '🇿🇲', nameZh: '赞比亚 (Zambia)', nameEn: 'Zambia', category: 'country' },
  { code: 'ml', flag: '🇲🇱', nameZh: '马里 (Mali)', nameEn: 'Mali', category: 'country' },
  { code: 'mw', flag: '🇲🇼', nameZh: '马拉维 (Malawi)', nameEn: 'Malawi', category: 'country' },
  { code: 'gn', flag: '🇬🇳', nameZh: '几内亚 (Guinea)', nameEn: 'Guinea', category: 'country' },
  { code: 'so', flag: '🇸🇴', nameZh: '索马里 (Somalia)', nameEn: 'Somalia', category: 'country' },
  { code: 'cd', flag: '🇨🇩', nameZh: '刚果(金) (DR Congo)', nameEn: 'DR Congo', category: 'country' },
  { code: 'cg', flag: '🇨🇬', nameZh: '刚果(布) (Congo)', nameEn: 'Congo', category: 'country' },
  { code: 'rw', flag: '🇷🇼', nameZh: '卢旺达 (Rwanda)', nameEn: 'Rwanda', category: 'country' },
  { code: 'bj', flag: '🇧🇯', nameZh: '贝宁 (Benin)', nameEn: 'Benin', category: 'country' },
  { code: 'tg', flag: '🇹🇬', nameZh: '多哥 (Togo)', nameEn: 'Togo', category: 'country' },
  { code: 'sl', flag: '🇸🇱', nameZh: '塞拉利昂 (Sierra Leone)', nameEn: 'Sierra Leone', category: 'country' },
  { code: 'ly', flag: '🇱🇾', nameZh: '利比亚 (Libya)', nameEn: 'Libya', category: 'country' },
  { code: 'lr', flag: '🇱🇷', nameZh: '利比里亚 (Liberia)', nameEn: 'Liberia', category: 'country' },
  { code: 'mr', flag: '🇲🇷', nameZh: '毛里塔尼亚 (Mauritania)', nameEn: 'Mauritania', category: 'country' },
  { code: 'cf', flag: '🇨🇫', nameZh: '中非共和国 (Central African Republic)', nameEn: 'Central African Republic', category: 'country' },
  { code: 'na', flag: '🇳🇦', nameZh: '纳米比亚 (Namibia)', nameEn: 'Namibia', category: 'country' },
  { code: 'bw', flag: '🇧🇼', nameZh: '博茨瓦纳 (Botswana)', nameEn: 'Botswana', category: 'country' },
  { code: 'ga', flag: '🇬🇦', nameZh: '加蓬 (Gabon)', nameEn: 'Gabon', category: 'country' },
  { code: 'mu', flag: '🇲🇺', nameZh: '毛里求斯 (Mauritius)', nameEn: 'Mauritius', category: 'country' },
  { code: 'sc', flag: '🇸🇨', nameZh: '塞舌尔 (Seychelles)', nameEn: 'Seychelles', category: 'country' },
  { code: 'cv', flag: '🇨🇻', nameZh: '佛得角 (Cape Verde)', nameEn: 'Cape Verde', category: 'country' },
  { code: 'dj', flag: '🇩🇯', nameZh: '吉布提 (Djibouti)', nameEn: 'Djibouti', category: 'country' },
  { code: 'gq', flag: '🇬🇶', nameZh: '赤道几内亚 (Equatorial Guinea)', nameEn: 'Equatorial Guinea', category: 'country' },
  { code: 'ne', flag: '🇳🇪', nameZh: '尼日尔 (Niger)', nameEn: 'Niger', category: 'country' },
  { code: 'bf', flag: '🇧🇫', nameZh: '布基纳法索 (Burkina Faso)', nameEn: 'Burkina Faso', category: 'country' },
  { code: 'td', flag: '🇹🇩', nameZh: '乍得 (Chad)', nameEn: 'Chad', category: 'country' },
  { code: 'er', flag: '🇪🇷', nameZh: '厄立特里亚 (Eritrea)', nameEn: 'Eritrea', category: 'country' },
  { code: 'bi', flag: '🇧🇮', nameZh: '布隆迪 (Burundi)', nameEn: 'Burundi', category: 'country' },
  { code: 'ls', flag: '🇱🇸', nameZh: '莱索托 (Lesotho)', nameEn: 'Lesotho', category: 'country' },
  { code: 'sz', flag: '🇸🇿', nameZh: '斯威士兰 (Eswatini)', nameEn: 'Eswatini', category: 'country' },
  { code: 'ss', flag: '🇸🇸', nameZh: '南苏丹 (South Sudan)', nameEn: 'South Sudan', category: 'country' },
  { code: 'km', flag: '🇰🇲', nameZh: '科摩罗 (Comoros)', nameEn: 'Comoros', category: 'country' },
  { code: 'st', flag: '🇸🇹', nameZh: '圣多美和普林西比 (Sao Tome and Principe)', nameEn: 'Sao Tome and Principe', category: 'country' },
  { code: 'gm', flag: '🇬🇲', nameZh: '冈比亚 (Gambia)', nameEn: 'Gambia', category: 'country' },
  { code: 'gw', flag: '🇬🇼', nameZh: '几内亚比绍 (Guinea-Bissau)', nameEn: 'Guinea-Bissau', category: 'country' },
];

/**
 * REGION_OPTIONS 里的 nameZh 采用「中文名 (English Name)」的中英对照格式，
 * 便于在选择器里同时按中英文搜索；但在纯展示场景（如流量排行、地图提示）
 * 只需要当前语言对应的名称。这里去掉中文名末尾的英文括号后缀。
 */
export const getRegionDisplayName = (option: RegionOption, isZh: boolean): string => {
  if (!isZh) return option.nameEn;
  return option.nameZh.replace(/\s*\([^()]*\)\s*$/, '').trim() || option.nameZh;
};

export const getRegionOption = (code: string): RegionOption => {
  if (!code) return { code: 'default', flag: '🌐', nameZh: '全球默认', nameEn: 'Global Default', category: 'global' };
  const clean = code.toLowerCase().trim();
  const found = REGION_OPTIONS.find((r) => r.code === clean);
  if (found) return found;

  // Generate dynamic fallback for any unexpected ISO code
  return {
    code: clean,
    flag: '🌐',
    nameZh: clean.toUpperCase(),
    nameEn: clean.toUpperCase(),
    category: 'country',
  };
};

interface FlagRegionSelectProps {
  value: string; // Comma separated, e.g. "us,eu,jp" or "default"
  onChange: (val: string) => void;
  label?: string;
  multiSelect?: boolean;
}

export const FlagRegionSelect: React.FC<FlagRegionSelectProps> = ({
  value,
  onChange,
  label,
  multiSelect = true,
}) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const dropdownRef = useRef<HTMLDivElement>(null);

  const selectedCodes = (value || 'default')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const handleToggleCode = (code: string) => {
    if (!multiSelect) {
      onChange(code);
      setIsOpen(false);
      return;
    }

    if (code === 'default') {
      onChange('default');
      return;
    }

    let newCodes = selectedCodes.filter((c) => c !== 'default');
    if (newCodes.includes(code)) {
      newCodes = newCodes.filter((c) => c !== code);
    } else {
      newCodes.push(code);
    }

    if (newCodes.length === 0) {
      onChange('default');
    } else {
      onChange(newCodes.join(','));
    }
  };

  const handleRemoveCode = (code: string, e: React.MouseEvent) => {
    e.stopPropagation();
    let newCodes = selectedCodes.filter((c) => c !== code);
    if (newCodes.length === 0) {
      onChange('default');
    } else {
      onChange(newCodes.join(','));
    }
  };

  const filtered = REGION_OPTIONS.filter((opt) => {
    const text = (opt.nameZh + ' ' + opt.nameEn + ' ' + opt.code).toLowerCase();
    return text.includes(search.toLowerCase().trim());
  });

  return (
    <div className="w-full space-y-1.5 relative" ref={dropdownRef}>
      {label && <label className="text-xs font-medium text-secondary block">{label}</label>}

      {/* Trigger Box */}
      <div
        onClick={() => setIsOpen(!isOpen)}
        className="min-h-10 bg-card border border-border hover:border-border-hover rounded-sm p-1.5 flex items-center justify-between gap-2 cursor-pointer transition-colors"
      >
        <div className="flex flex-wrap items-center gap-1.5 max-h-24 overflow-y-auto">
          {selectedCodes.map((code) => {
            const opt = getRegionOption(code);
            return (
              <span
                key={code}
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-bg-subtle border border-border text-xs font-mono text-primary select-none"
              >
                <span>{opt.flag}</span>
                <span>{isZh ? opt.nameZh.split(' ')[0] : opt.nameEn}</span>
                {multiSelect && selectedCodes.length > 1 && (
                  <button
                    type="button"
                    onClick={(e) => handleRemoveCode(code, e)}
                    className="hover:text-red-500 transition-colors ml-0.5"
                  >
                    <X className="w-3 h-3" />
                  </button>
                )}
              </span>
            );
          })}
        </div>

        <ChevronDown className={`w-3.5 h-3.5 text-secondary flex-shrink-0 transition-transform ${isOpen ? 'rotate-180' : ''}`} />
      </div>

      {/* Dropdown Panel */}
      {isOpen && (
        <div className="absolute left-0 right-0 top-full mt-1 bg-card border border-border rounded-sm shadow-2xl z-50 animate-in fade-in zoom-in-95 duration-100 p-2 space-y-2">
          {/* Search Box */}
          <div className="flex items-center gap-1.5 px-2 py-1.5 bg-bg-subtle border border-border rounded">
            <Search className="w-3.5 h-3.5 text-tertiary" />
            <input
              type="text"
              placeholder={isZh ? "搜索国家代码、中文或英文名称 (如 cn, us, 日本, 德国)..." : "Search country code or name (e.g. cn, us, Japan)..."}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full bg-transparent text-xs text-primary focus:outline-none font-mono"
              autoFocus
            />
            {search && (
              <button type="button" onClick={() => setSearch('')} className="text-secondary hover:text-primary">
                <X className="w-3 h-3" />
              </button>
            )}
          </div>

          {/* Options List */}
          <div className="max-h-64 overflow-y-auto space-y-0.5 py-1 divide-y divide-border/20">
            {filtered.map((opt) => {
              const isSelected = selectedCodes.includes(opt.code);
              return (
                <div
                  key={opt.code}
                  onClick={() => handleToggleCode(opt.code)}
                  className={`flex items-center justify-between px-2.5 py-1.5 rounded text-xs font-mono cursor-pointer transition-colors ${
                    isSelected ? 'bg-primary text-bg font-semibold' : 'hover:bg-bg-subtle text-primary'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span className="text-sm">{opt.flag}</span>
                    <span>{isZh ? opt.nameZh : opt.nameEn}</span>
                    <span className={`text-[10px] px-1 py-0.2 rounded border ${isSelected ? 'border-bg text-bg/80' : 'border-border text-tertiary'}`}>
                      {opt.code.toUpperCase()}
                    </span>
                  </div>
                  {isSelected && <Check className="w-3.5 h-3.5 flex-shrink-0" />}
                </div>
              );
            })}

            {filtered.length === 0 && (
              <div className="text-center text-secondary py-4 text-xs font-mono">
                {isZh ? '未找到匹配的国家 / 地区' : 'No matching regions found'}
              </div>
            )}
          </div>

          {multiSelect && (
            <div className="pt-2 border-t border-border flex items-center justify-between text-[11px] font-mono text-tertiary">
              <span>
                {isZh ? `已选择 ${selectedCodes.length} 个地区` : `${selectedCodes.length} regions selected`}
              </span>
              <button
                type="button"
                onClick={() => onChange('default')}
                className="text-blue-500 hover:underline"
              >
                {isZh ? '重置为默认' : 'Reset to Default'}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export const FlagRegionBadge: React.FC<{ codeString: string }> = ({ codeString }) => {
  const { language } = useI18n();
  const isZh = language === 'zh-CN';
  const codes = (codeString || 'default').split(',').map((s) => s.trim()).filter(Boolean);

  return (
    <div className="flex flex-wrap items-center gap-1">
      {codes.map((c) => {
        const opt = getRegionOption(c);
        return (
          <span
            key={c}
            className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-bg-subtle border border-border text-[11px] font-mono text-secondary"
            title={isZh ? opt.nameZh : opt.nameEn}
          >
            <span>{opt.flag}</span>
            <span className="truncate max-w-[120px]">{isZh ? opt.nameZh.split(' ')[0] : opt.nameEn}</span>
          </span>
        );
      })}
    </div>
  );
};
