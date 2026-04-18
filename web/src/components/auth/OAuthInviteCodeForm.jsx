/*
Copyright (C) 2025 QuantumNous

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

import React, { useContext, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Button, Card, Form, Typography } from '@douyinfe/semi-ui';
import { IconKey } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import {
  API,
  getLogo,
  getSystemName,
  setUserData,
  showError,
  showSuccess,
  updateAPI,
} from '../../helpers';
import { UserContext } from '../../context/User';

const { Text, Title } = Typography;

const providerLabelMap = {
  github: 'GitHub',
  discord: 'Discord',
  oidc: 'OIDC',
  linuxdo: 'LinuxDo',
};

const OAuthInviteCodeForm = () => {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const [, userDispatch] = useContext(UserContext);
  const [inviteCode, setInviteCode] = useState(
    localStorage.getItem('invite_code') || '',
  );
  const [loading, setLoading] = useState(false);

  const logo = getLogo();
  const systemName = getSystemName();
  const provider = (searchParams.get('provider') || '').trim().toLowerCase();
  const providerLabel = useMemo(() => {
    if (!provider) {
      return '';
    }
    return providerLabelMap[provider] || provider;
  }, [provider]);

  const handleSubmit = async () => {
    if (!provider) {
      showError(t('缺少第三方登录来源，请重新发起登录'));
      navigate('/login');
      return;
    }

    const trimmedInviteCode = inviteCode.trim();
    if (!trimmedInviteCode) {
      showError(t('请输入邀请码'));
      return;
    }

    localStorage.setItem('invite_code', trimmedInviteCode);
    setLoading(true);
    try {
      const res = await API.post(`/api/oauth/${provider}/complete`, {
        invite_code: trimmedInviteCode,
      });
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }

      userDispatch({ type: 'login', payload: data });
      localStorage.setItem('user', JSON.stringify(data));
      localStorage.removeItem('invite_code');
      setUserData(data);
      updateAPI();
      showSuccess(t('登录成功！'));
      navigate('/console/token');
    } catch (error) {
      showError(error.message || t('注册失败，请重试'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className='relative overflow-hidden bg-gray-100 flex items-center justify-center py-12 px-4 sm:px-6 lg:px-8'>
      <div
        className='blur-ball blur-ball-indigo'
        style={{ top: '-80px', right: '-80px', transform: 'none' }}
      />
      <div
        className='blur-ball blur-ball-teal'
        style={{ top: '50%', left: '-120px' }}
      />
      <div className='w-full max-w-sm mt-[60px]'>
        <div className='flex flex-col items-center'>
          <div className='w-full max-w-md'>
            <div className='flex items-center justify-center mb-6 gap-2'>
              <img src={logo} alt='Logo' className='h-10 rounded-full' />
              <Title heading={3} className='!text-gray-800'>
                {systemName}
              </Title>
            </div>

            <Card className='border-0 !rounded-2xl overflow-hidden'>
              <div className='flex justify-center pt-6 pb-2'>
                <Title heading={3} className='text-gray-800 dark:text-gray-200'>
                  {t('填写邀请码')}
                </Title>
              </div>
              <div className='px-2 py-8'>
                <div className='mb-6 text-center'>
                  <Text className='text-gray-600'>
                    {providerLabel
                      ? t(`首次使用 ${providerLabel} 登录，需要先填写邀请码完成注册。`)
                      : t('请填写邀请码完成首次注册。')}
                  </Text>
                </div>

                <Form className='space-y-3'>
                  <Form.Input
                    field='invite_code'
                    label={t('邀请码')}
                    placeholder={t('请输入邀请码')}
                    name='invite_code'
                    value={inviteCode}
                    onChange={(value) => setInviteCode(value)}
                    prefix={<IconKey />}
                  />

                  <div className='space-y-2 pt-2'>
                    <Button
                      theme='solid'
                      className='w-full !rounded-full'
                      type='primary'
                      htmlType='submit'
                      onClick={handleSubmit}
                      loading={loading}
                      disabled={!provider}
                    >
                      {t('提交邀请码并继续')}
                    </Button>
                  </div>
                </Form>

                <div className='mt-6 text-center text-sm'>
                  <Text>
                    <Link
                      to='/login'
                      className='text-blue-600 hover:text-blue-800 font-medium'
                    >
                      {t('返回登录')}
                    </Link>
                  </Text>
                </div>
              </div>
            </Card>
          </div>
        </div>
      </div>
    </div>
  );
};

export default OAuthInviteCodeForm;
